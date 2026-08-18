// Package blocklist holds the preset catalog and the permanent allowlist.
// Presets are seed data, not behaviour: a user edit forks a preset into a custom list.
package blocklist

import "sort"

type List struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Domains   []string `json:"domains"`
	Processes []string `json:"processes,omitempty"`
	// Composes names other list IDs whose contents are folded in. preset.work is
	// video ∪ doomscroll ∪ gaming and should not restate their domains.
	Composes []string `json:"composes,omitempty"`

	// DefaultDeny inverts the list: everything is refused except the permanent
	// allowlist and the user's own allowlist. "Bedtime" and "Offline" mean
	// "everything", which a domain list cannot express — enumerating eleven
	// presets and calling it the internet is a lie the user only discovers by
	// successfully browsing.
	//
	// Honoured by the DNS sink alone. hosts cannot express it, and a WFP
	// default-deny is a bricked network stack rather than a blocker.
	DefaultDeny bool `json:"defaultDeny,omitempty"`

	// AllowPaths carves exceptions out of a blocked domain by URL path. The
	// network layer cannot honour these — HTTPS means it sees `youtube.com` and
	// never `/watch` vs `/@channel` — so they are enforced only by the browser
	// extension, and only for browsers that have it installed.
	AllowPaths map[string][]string `json:"allowPaths,omitempty"`
}

// Catalog is a set of lists addressable by id.
type Catalog map[string]List

func (c Catalog) IDs() []string {
	ids := make([]string, 0, len(c))
	for id := range c {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Resolve flattens a list's own entries plus everything it composes, with the
// permanent allowlist subtracted. Cycles terminate; a missing id contributes nothing.
func (c Catalog) Resolve(id string) (domains, processes []string) {
	seen := map[string]bool{}
	ds, ps := map[string]bool{}, map[string]bool{}

	var walk func(string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		l, ok := c[id]
		if !ok {
			return
		}
		for _, d := range l.Domains {
			// Allowlist subtraction happens here, at the only place lists become
			// concrete, so no caller can forget it.
			if !Allowed(d) {
				ds[d] = true
			}
		}
		for _, p := range l.Processes {
			ps[p] = true
		}
		for _, sub := range l.Composes {
			walk(sub)
		}
	}
	walk(id)
	return keys(ds), keys(ps)
}

// ResolvePaths collects the path exceptions for a list and everything it
// composes. A domain with no entry has no exceptions: the whole domain is blocked.
//
// Exceptions do not compose across sources — if any active list blocks a domain
// outright, that wins. Otherwise "study allows /@channel" would quietly unblock
// channels for a plain `work` session too.
func (c Catalog) ResolvePaths(ids []string) map[string][]string {
	out := map[string][]string{}
	blockedOutright := map[string]bool{}

	for _, id := range ids {
		l, ok := c[id]
		if !ok {
			continue
		}
		domains, _ := c.Resolve(id)
		for _, d := range domains {
			if _, exempt := l.AllowPaths[d]; !exempt {
				blockedOutright[d] = true
			}
		}
		for d, pats := range l.AllowPaths {
			if Allowed(d) {
				continue
			}
			out[d] = append(out[d], pats...)
		}
	}
	for d := range out {
		if blockedOutright[d] {
			delete(out, d)
		}
	}
	return out
}

// DefaultDenies reports whether a list, or anything it composes, inverts the
// blocklist. Walks the same graph Resolve does, with the same cycle guard.
func (c Catalog) DefaultDenies(id string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(id string) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		l, ok := c[id]
		if !ok {
			return false
		}
		if l.DefaultDeny {
			return true
		}
		for _, sub := range l.Composes {
			if walk(sub) {
				return true
			}
		}
		return false
	}
	return walk(id)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Presets is the seed catalog from CLAUDE.md.
func Presets() Catalog {
	return Catalog{
		"preset.adult": {ID: "preset.adult", Name: "Adult content", Domains: []string{
			"pornhub.com", "xvideos.com", "xnxx.com", "xhamster.com", "redtube.com",
			"youporn.com", "onlyfans.com", "chaturbate.com", "stripchat.com", "spankbang.com",
		}},
		"preset.gambling": {ID: "preset.gambling", Name: "Gambling", Domains: []string{
			"bet365.com", "draftkings.com", "fanduel.com", "stake.com", "pokerstars.com",
			"bovada.lv", "betmgm.com", "williamhill.com", "roobet.com", "csgoempire.com",
		}},
		"preset.shopping": {ID: "preset.shopping", Name: "Shopping", Domains: []string{
			"amazon.com", "ebay.com", "aliexpress.com", "temu.com", "shein.com",
			"etsy.com", "wish.com", "bestbuy.com", "walmart.com",
		}},
		"preset.doomscroll": {ID: "preset.doomscroll", Name: "Doomscroll", Domains: []string{
			"x.com", "twitter.com", "reddit.com", "instagram.com", "facebook.com",
			"tiktok.com", "news.ycombinator.com", "9gag.com", "tumblr.com",
			"threads.net", "bsky.app", "linkedin.com", "quora.com",
		}},
		"preset.video": {ID: "preset.video", Name: "Video", Domains: []string{
			"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com",
			"twitch.tv", "netflix.com", "nflxvideo.net", "hulu.com",
			"disneyplus.com", "primevideo.com", "vimeo.com", "dailymotion.com",
			"crunchyroll.com", "tiktok.com",
		}},
		"preset.gaming": {ID: "preset.gaming", Name: "Gaming",
			Domains: []string{
				"steampowered.com", "steamcommunity.com", "epicgames.com",
				"riotgames.com", "battle.net", "blizzard.com", "ea.com",
				"ubisoft.com", "roblox.com", "minecraft.net",
			},
			Processes: []string{
				"steam.exe", "epicgameslauncher.exe", "riotclientservices.exe",
				"battle.net.exe", "leagueoflegends.exe", "valorant.exe",
				"eadesktop.exe", "ubisoftconnect.exe", "robloxplayerbeta.exe",
			}},
		"preset.delivery": {ID: "preset.delivery", Name: "Food delivery", Domains: []string{
			"ubereats.com", "doordash.com", "grubhub.com", "skipthedishes.com",
			"justeat.com", "deliveroo.co.uk", "postmates.com",
		}},
		"preset.work": {ID: "preset.work", Name: "Work",
			Composes: []string{"preset.video", "preset.doomscroll", "preset.gaming"}},
		// Study is work minus the parts of YouTube that are course material. The
		// feed, watch pages and Shorts stay blocked; a channel or playlist you
		// navigated to deliberately does not.
		"preset.study": {ID: "preset.study", Name: "Study",
			Composes: []string{"preset.video", "preset.doomscroll", "preset.gaming"},
			AllowPaths: map[string][]string{
				"youtube.com": {`^/@`, `^/channel/`, `^/playlist`, `^/c/`, `^/results`},
			}},
		// "Everything except the allowlist", which is what these two have always
		// claimed and could not previously do. The composed lists stay so that
		// the hosts layer — which cannot express default-deny — still blocks the
		// obvious things, and so the effective set is never empty.
		"preset.bedtime": {ID: "preset.bedtime", Name: "Bedtime", DefaultDeny: true,
			Composes: []string{"preset.video", "preset.doomscroll", "preset.gaming", "preset.shopping", "preset.adult", "preset.gambling"}},
		"preset.offline": {ID: "preset.offline", Name: "Offline", DefaultDeny: true,
			Composes: []string{"preset.bedtime", "preset.delivery"}},
	}
}
