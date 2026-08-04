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
		"preset.study": {ID: "preset.study", Name: "Study",
			Composes: []string{"preset.video", "preset.doomscroll", "preset.gaming"}},
		// preset.bedtime and preset.offline mean "everything except the allowlist",
		// which the domain-list model cannot express. Phase 6 gives them a
		// default-deny flag; until then they are the union of everything named.
		"preset.bedtime": {ID: "preset.bedtime", Name: "Bedtime",
			Composes: []string{"preset.video", "preset.doomscroll", "preset.gaming", "preset.shopping", "preset.adult", "preset.gambling"}},
		"preset.offline": {ID: "preset.offline", Name: "Offline",
			Composes: []string{"preset.bedtime", "preset.delivery"}},
	}
}
