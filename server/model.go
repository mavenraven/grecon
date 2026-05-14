package server

var modelDisplayNames = map[string]string{
	"claude-opus-4-6":            "Opus 4.6",
	"claude-sonnet-4-6":          "Sonnet 4.6",
	"claude-sonnet-4-5-20250514": "Sonnet 4.5",
	"claude-haiku-4-5-20251001":  "Haiku 4.5",
	"claude-opus-4-20250514":     "Opus 4",
	"claude-sonnet-4-20250514":   "Sonnet 4",
}

var modelContextWindows = map[string]uint64{
	"claude-opus-4-6": 1_000_000,
}

var displayNameToID = map[string]string{
	"Opus 4.6":               "claude-opus-4-6",
	"Opus 4.6 (1M context)":  "claude-opus-4-6",
	"Sonnet 4.6":             "claude-sonnet-4-6",
	"Sonnet 4.5":             "claude-sonnet-4-5-20250514",
	"Haiku 4.5":              "claude-haiku-4-5-20251001",
	"Opus 4":                 "claude-opus-4-20250514",
	"Sonnet 4":               "claude-sonnet-4-20250514",
}

func ModelDisplayName(id string) string {
	if name, ok := modelDisplayNames[id]; ok {
		return name
	}
	return id
}

func ModelContextWindow(id string) uint64 {
	if w, ok := modelContextWindows[id]; ok {
		return w
	}
	return 200_000
}

func ModelIDFromDisplayName(display string) (string, bool) {
	id, ok := displayNameToID[display]
	return id, ok
}

func FormatModelWithEffort(modelID, effort string) string {
	name := ModelDisplayName(modelID)
	if effort == "" || effort == "default" {
		return name
	}
	return name + " (" + effort + ")"
}
