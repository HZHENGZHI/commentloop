package coderepo

import "testing"

func TestParseGitHub(t *testing.T) {
	cases := map[string]string{
		"https://github.com/owner/repo":       "https://github.com/owner/repo.git",
		"https://github.com/owner/repo.git":   "https://github.com/owner/repo.git",
		"owner/repo":                          "https://github.com/owner/repo.git",
		"git@github.com:owner/repo.git":       "git@github.com:owner/repo.git",
		"ssh://git@github.com/owner/repo.git": "git@github.com:owner/repo.git",
		"http://github.com/owner/repo/":       "https://github.com/owner/repo.git",
	}
	for input, want := range cases {
		source, err := ParseGitHub(input)
		if err != nil {
			t.Errorf("ParseGitHub(%q): %v", input, err)
			continue
		}
		if source.CloneURL != want {
			t.Errorf("ParseGitHub(%q).CloneURL = %q, want %q", input, source.CloneURL, want)
		}
	}
	for _, input := range []string{"https://gitlab.com/owner/repo", "github.com/owner", "https://github.com/owner/repo/tree/main"} {
		if _, err := ParseGitHub(input); err == nil {
			t.Errorf("ParseGitHub(%q) should fail", input)
		}
	}
}
