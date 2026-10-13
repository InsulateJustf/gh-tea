package tea

import (
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strings"

	"github.com/InsulateJustf/gh-tea/pkg/config"
)

// readGitRemotes is the process boundary; tests replace it with remote output.
var readGitRemotes = func() ([]byte, error) {
	return exec.Command("git", "remote", "-v").Output()
}

// Resolve selects an authenticated Tea and a repository for this invocation.
// Explicit flags override inference; only remotes belonging to the selected
// Tea can supply a repository. Git failures mean no remote context.
func Resolve(cfg config.Config, use, repoFlag string) (item config.TeaItem, owner, name string, err error) {
	item, owner, name, err = resolveSelection(cfg, use, repoFlag == "")
	if err != nil {
		return
	}
	if repoFlag != "" {
		owner, name, err = splitRepo(repoFlag)
		if err != nil {
			return
		}
	}
	if owner == "" || name == "" {
		err = fmt.Errorf("specify --repo or run inside a Gitea repo clone")
		return
	}
	if item.Token == "" {
		err = fmt.Errorf("run: gh tea auth login --use %s", item.Name)
	}
	return
}

// ResolveTea selects a host without requiring a repository or stored token.
// Auth login/logout use this same Tea precedence without repo prerequisites.
func ResolveTea(cfg config.Config, use string) (config.TeaItem, error) {
	item, _, _, err := resolveSelection(cfg, use, false)
	return item, err
}

func resolveSelection(cfg config.Config, use string, needRepo bool) (config.TeaItem, string, string, error) {
	key := config.TeaName(use)
	if use != "" {
		if _, ok := cfg.Tea[key]; !ok {
			return config.TeaItem{}, "", "", fmt.Errorf("no such tea name: %s", use)
		}
	}
	// Deduplicate fetch/push lines, aliases and repositories per Tea.
	matches := make(map[config.TeaName]map[string]struct{})
	if use == "" || needRepo {
		data, err := readGitRemotes()
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					continue
				}
				remote := parseRemote(fields[1])
				if remote == nil {
					continue
				}
				for teaName, item := range cfg.Tea {
					if use != "" && teaName != key {
						continue
					}
					repo, ok := remoteRepo(remote, item.Host)
					if !ok {
						continue
					}
					if matches[teaName] == nil {
						matches[teaName] = make(map[string]struct{})
					}
					matches[teaName][repo] = struct{}{}
				}
			}
		}
	}
	if use == "" {
		switch len(matches) {
		case 0:
			key = cfg.Default
		case 1:
			for matched := range matches {
				key = matched
			}
		default:
			names := make([]string, 0, len(matches))
			for matched := range matches {
				names = append(names, string(matched))
			}
			sort.Strings(names)
			return config.TeaItem{}, "", "", fmt.Errorf("multiple teas match git remotes: %s; specify --use", strings.Join(names, ", "))
		}
	}
	if key == "" {
		return config.TeaItem{}, "", "", fmt.Errorf("no tea configured; run: gh tea new <name> <host>")
	}
	item, ok := cfg.Tea[key]
	if !ok {
		return config.TeaItem{}, "", "", fmt.Errorf("default tea %q is missing from the config", key)
	}
	item.Name = string(key)
	if item.Host == "" {
		return item, "", "", fmt.Errorf("tea %q has no host; run: gh tea new %s <host>", key, key)
	}
	if _, err := config.NormalizeHost(item.Host); err != nil {
		return item, "", "", err
	}
	if !needRepo {
		return item, "", "", nil
	}
	repos := matches[key]
	if len(repos) > 1 {
		return item, "", "", fmt.Errorf("multiple repositories match tea %q; specify --repo", key)
	}
	for repo := range repos {
		owner, name, err := splitRepo(repo)
		return item, owner, name, err
	}
	return item, "", "", nil
}

func splitRepo(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(repo, " \t\r\n?#") || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", "", fmt.Errorf("invalid --repo %q: expected owner/name", repo)
	}
	return parts[0], parts[1], nil
}

func parseRemote(raw string) *url.URL {
	if !strings.Contains(raw, "://") {
		// Git's scp-like SSH syntax: [user@]host:owner/repo.git.
		host, path, ok := "", "", false
		hostStart := strings.LastIndex(raw, "@") + 1
		if hostStart < len(raw) && raw[hostStart] == '[' {
			if end := strings.IndexByte(raw[hostStart:], ']'); end >= 0 {
				separator := hostStart + end + 1
				if separator < len(raw) && raw[separator] == ':' {
					host, path, ok = raw[:separator], raw[separator+1:], true
				}
			}
		} else {
			host, path, ok = strings.Cut(raw, ":")
		}
		if !ok || strings.Contains(host, "/") {
			return nil
		}
		raw = "ssh://" + host + "/" + path
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil
	}
	switch u.Scheme {
	case "http", "https", "ssh":
		return u
	default:
		return nil
	}
}

func remoteRepo(remote *url.URL, host string) (string, bool) {
	normalized, err := config.NormalizeHost(host)
	if err != nil {
		return "", false
	}
	base, err := url.Parse(normalized)
	if err != nil {
		return "", false
	}
	if remote.Scheme == "ssh" {
		// SSH and the web service often use different schemes and ports.
		if !strings.EqualFold(remote.Hostname(), base.Hostname()) {
			return "", false
		}
	} else {
		remoteHost, err := config.NormalizeHost(remote.Scheme + "://" + remote.Host)
		if err != nil || remoteHost != base.Scheme+"://"+base.Host {
			return "", false
		}
	}
	path := strings.TrimPrefix(remote.Path, "/")
	if remote.Scheme != "ssh" && base.Path != "" {
		prefix := strings.Trim(base.Path, "/") + "/"
		if !strings.HasPrefix(path, prefix) {
			return "", false
		}
		path = strings.TrimPrefix(path, prefix)
	}
	repo := strings.TrimSuffix(path, ".git")
	if _, _, err := splitRepo(repo); err != nil {
		return "", false
	}
	return repo, true
}
