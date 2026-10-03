package toolargs

import (
	"net/url"
	"regexp"
	"sort"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
)

var taskURL = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)

// TaskURLs returns explicit HTTP targets from the latest user instruction.
// Observations from tools and pages cannot add authorization here.
func TaskURLs(messages []*aop.Message) []string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.GetRole() != "user" || m.GetName() != "" {
			continue
		}
		seen := map[string]bool{}
		for _, raw := range taskURL.FindAllString(provider.MessageText(m), 8) {
			u, err := url.Parse(raw)
			if err == nil && u.Host != "" && u.User == nil && len(raw) <= 2048 {
				seen[raw] = true
			}
		}
		urls := make([]string, 0, len(seen))
		for raw := range seen {
			urls = append(urls, raw)
		}
		sort.Strings(urls)
		return urls
	}
	return nil
}
