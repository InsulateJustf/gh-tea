package tea

import (
	"errors"
	"strings"
	"testing"

	"github.com/InsulateJustf/gh-tea/pkg/config"
)

func TestResolveExplicit(t *testing.T) {
	cfg := config.Config{Tea: config.Tea{"work": {Host: "https://tea.example", Token: "test-token"}}}
	tests := []struct{ name, use, repo, wantErr string }{
		{name: "explicit Tea and repo", use: "work", repo: "alice/project"},
		{name: "unknown Tea never falls back", use: "missing", repo: "alice/project", wantErr: "no such tea name: missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item, owner, name, err := Resolve(cfg, tt.use, tt.repo)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if item.Name != "work" || item.Host != "https://tea.example" || owner != "alice" || name != "project" {
				t.Fatalf("resolved = %+v, %s/%s", item, owner, name)
			}
		})
	}
}

func TestResolveRemote(t *testing.T) {
	original := readGitRemotes
	t.Cleanup(func() { readGitRemotes = original })
	readGitRemotes = func() ([]byte, error) {
		return []byte("origin\tgit@TEA.EXAMPLE:alice/project.git (fetch)\norigin\tgit@TEA.EXAMPLE:alice/project.git (push)\n"), nil
	}
	cfg := config.Config{
		Default: "fallback",
		Tea: config.Tea{
			"fallback": {Host: "https://other.example", Token: "fallback-token"},
			"work":     {Host: "https://tea.example/", Token: "work-token"},
		},
	}
	item, owner, name, err := Resolve(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if item.Name != "work" || owner != "alice" || name != "project" {
		t.Fatalf("resolved = %+v, %s/%s", item, owner, name)
	}
}

func TestResolvePrecedenceAndErrors(t *testing.T) {
	original := readGitRemotes
	t.Cleanup(func() { readGitRemotes = original })
	tests := []struct {
		name, remote, use, repo, wantTea, wantRepo, wantErr string
		gitErr                                              error
		change                                              func(*config.Config)
	}{
		{name: "https normalized host", remote: "origin https://TEA.EXAMPLE/alice/project.git (fetch)", wantTea: "work", wantRepo: "alice/project"},
		{name: "ssh URL with independent port", remote: "origin ssh://git@tea.example:2222/alice/project.git (fetch)", wantTea: "work", wantRepo: "alice/project"},
		{name: "http with web port and subpath", remote: "origin http://TEA.EXAMPLE:3000/gitea/alice/project.git (fetch)", wantTea: "work", wantRepo: "alice/project", change: func(c *config.Config) {
			i := c.Tea["work"]
			i.Host = "http://tea.example:3000/gitea/"
			c.Tea["work"] = i
		}},
		{name: "repo override preserves remote Tea", remote: "origin https://tea.example/alice/project.git (fetch)", repo: "bob/override", wantTea: "work", wantRepo: "bob/override"},
		{name: "use override with matching repo", use: "fallback", remote: "origin https://tea.example/alice/project.git (fetch)\nsecond https://other.example/bob/selected.git (fetch)", wantTea: "fallback", wantRepo: "bob/selected"},
		{name: "explicit flags override ambiguity", use: "work", repo: "bob/override", remote: "origin https://tea.example/alice/project.git (fetch)\nsecond https://other.example/bob/other.git (fetch)", wantTea: "work", wantRepo: "bob/override"},
		{name: "explicit use never borrows another host repo", use: "fallback", remote: "origin https://tea.example/alice/project.git (fetch)", wantErr: "specify --repo or run inside a Gitea repo clone"},
		{name: "multiple matched hosts", remote: "origin https://tea.example/alice/project.git (fetch)\nsecond https://other.example/bob/other.git (fetch)", repo: "alice/project", wantErr: "multiple teas match git remotes: fallback, work; specify --use"},
		{name: "same host multiple Teas", remote: "origin git@tea.example:alice/project.git (fetch)", wantErr: "multiple teas match git remotes: alias, work; specify --use", change: func(c *config.Config) { c.Tea["alias"] = c.Tea["work"] }},
		{name: "explicit use disambiguates same host", use: "work", remote: "origin git@tea.example:alice/project.git (fetch)", wantTea: "work", wantRepo: "alice/project", change: func(c *config.Config) { c.Tea["alias"] = c.Tea["work"] }},
		{name: "unmatched host uses default", remote: "origin https://unrelated.example/a/b.git (fetch)", repo: "alice/project", wantTea: "fallback", wantRepo: "alice/project"},
		{name: "GitHub clone uses default with explicit repo", remote: "origin git@github.com:alice/github.git (fetch)", repo: "alice/project", wantTea: "fallback", wantRepo: "alice/project"},
		{name: "GitHub clone never infers repo", remote: "origin https://github.com/alice/project.git (fetch)", wantErr: "specify --repo or run inside a Gitea repo clone"},
		{name: "outside git uses default", gitErr: errors.New("not a git repository"), repo: "alice/project", wantTea: "fallback", wantRepo: "alice/project"},
		{name: "missing repo outside git", gitErr: errors.New("not a git repository"), wantErr: "specify --repo or run inside a Gitea repo clone"},
		{name: "invalid and local remotes ignored", remote: "origin /tmp/a/b (fetch)\nsecond file:///tmp/a/b (fetch)\nthird https://tea.example/not-a-repo (fetch)", repo: "alice/project", wantTea: "fallback", wantRepo: "alice/project"},
		{name: "missing token", use: "work", repo: "alice/project", wantErr: "run: gh tea auth login --use work", change: func(c *config.Config) { i := c.Tea["work"]; i.Token = ""; c.Tea["work"] = i }},
		{name: "missing host", use: "work", repo: "alice/project", wantErr: "tea \"work\" has no host", change: func(c *config.Config) { i := c.Tea["work"]; i.Host = ""; c.Tea["work"] = i }},
		{name: "invalid host", use: "work", repo: "alice/project", wantErr: "must start with http:// or https://", change: func(c *config.Config) { i := c.Tea["work"]; i.Host = "tea.example"; c.Tea["work"] = i }},
		{name: "no default", repo: "alice/project", wantErr: "no tea configured", change: func(c *config.Config) { c.Default = "" }},
		{name: "dangling default", repo: "alice/project", wantErr: "default tea \"missing\" is missing", change: func(c *config.Config) { c.Default = "missing" }},
		{name: "malformed explicit repo", use: "work", repo: "owner/repo/extra", wantErr: "expected owner/name"},
		{name: "empty owner", use: "work", repo: "/repo", wantErr: "expected owner/name"},
		{name: "multiple repos require explicit repo", remote: "origin https://tea.example/alice/project.git (fetch)\nsecond https://tea.example/bob/other.git (fetch)", wantErr: "multiple repositories match tea \"work\"; specify --repo"},
		{name: "explicit repo disambiguates repositories", remote: "origin https://tea.example/alice/project.git (fetch)\nsecond https://tea.example/bob/other.git (fetch)", repo: "carol/chosen", wantTea: "work", wantRepo: "carol/chosen"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readGitRemotes = func() ([]byte, error) { return []byte(tt.remote), tt.gitErr }
			cfg := config.Config{Default: "fallback", Tea: config.Tea{
				"fallback": {Host: "https://other.example", Token: "fallback-token"},
				"work":     {Name: "stale-name", Host: "https://tea.example/", Token: "work-token"},
			}}
			if tt.change != nil {
				tt.change(&cfg)
			}
			item, owner, name, err := Resolve(cfg, tt.use, tt.repo)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if item.Name != tt.wantTea || owner+"/"+name != tt.wantRepo {
				t.Fatalf("resolved = %s %s/%s, want %s %s", item.Name, owner, name, tt.wantTea, tt.wantRepo)
			}
		})
	}
}

func TestResolveTeaWithoutRepositoryOrToken(t *testing.T) {
	original := readGitRemotes
	t.Cleanup(func() { readGitRemotes = original })
	readGitRemotes = func() ([]byte, error) {
		return []byte("origin git@tea.example:alice/project.git (fetch)\nsecond git@tea.example:bob/other.git (fetch)"), nil
	}
	cfg := config.Config{Tea: config.Tea{"work": {Host: "https://tea.example"}}}
	item, err := ResolveTea(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if item.Name != "work" || item.Host != "https://tea.example" || item.Token != "" {
		t.Fatalf("host-only selection = %+v", item)
	}
}
