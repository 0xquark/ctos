package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// envRef matches ${VAR} and ${VAR:-default}. Bare $VAR is deliberately not
// matched, so values like a strftime pattern or a literal price survive intact.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expandTree rewrites every string scalar in a YAML document, expanding
// environment references and a leading ~. It reports the first unset variable
// that has no default, so a missing token fails at load rather than at use.
func expandTree(n *yaml.Node, file string) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode && (n.Tag == "!!str" || n.Tag == "") {
		v, err := expandString(n.Value)
		if err != nil {
			return fmt.Errorf("%s:%d: %w", file, n.Line, err)
		}
		n.Value = ExpandPath(v)
	}
	for _, c := range n.Content {
		if err := expandTree(c, file); err != nil {
			return err
		}
	}
	return nil
}

// expandString substitutes ${VAR} and ${VAR:-default} from the environment.
// An unset variable with no default is an error rather than an empty string:
// silently blanking a token produces a confusing failure much later.
func expandString(s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	var missing []string
	out := envRef.ReplaceAllStringFunc(s, func(match string) string {
		m := envRef.FindStringSubmatch(match)
		name, def := m[1], m[2]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		if strings.Contains(match, ":-") {
			return def
		}
		missing = append(missing, name)
		return ""
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("environment variable %s is not set "+
			"(use ${%s:-default} to make it optional)",
			strings.Join(missing, ", "), missing[0])
	}
	return out, nil
}
