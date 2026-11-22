package cmdutil

import (
	"context"
	"strings"

	"github.com/mwcli/mwcli/api"
)

// NamespacedTitle returns name as a title in namespace id: as it is when it
// starts with a name of that namespace, local, canonical or an alias, else
// canonical, a colon and name. So a category or file whose name has a colon,
// such as "A: B", works without its prefix too.
func NamespacedTitle(ctx context.Context, c *api.Client, name string, id int, canonical string) (string, error) {
	name = strings.TrimSpace(name)
	prefix, _, ok := strings.Cut(name, ":")
	if !ok {
		return canonical + ":" + name, nil
	}
	names, err := c.NamespaceNames(ctx, id)
	if err != nil {
		return "", err
	}
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", " ")) }
	for _, n := range names {
		if n != "" && norm(n) == norm(prefix) {
			return name, nil
		}
	}
	return canonical + ":" + name, nil
}
