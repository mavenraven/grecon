package client

import (
	"fmt"
	"math/rand"
)

var adjectives = []string{
	"bold", "brave", "bright", "calm", "clear",
	"cool", "crisp", "deft", "eager", "fair",
	"fast", "firm", "fond", "free", "glad",
	"gold", "grand", "keen", "kind", "late",
	"lean", "light", "live", "loud", "lucid",
	"mild", "neat", "noble", "pale", "plain",
	"plush", "prime", "proud", "pure", "quick",
	"quiet", "rare", "raw", "rich", "ripe",
	"sharp", "sleek", "slim", "smart", "snug",
	"soft", "solid", "spare", "stark", "still",
	"stone", "stout", "sure", "swift", "tall",
	"tame", "taut", "thin", "tidy", "trim",
	"true", "vast", "vivid", "warm", "wide",
	"wild", "wise", "young", "zany", "zen",
}

var nouns = []string{
	"anchor", "aspen", "atlas", "aurora", "basin",
	"birch", "blaze", "bloom", "bolt", "brook",
	"cairn", "cedar", "cliff", "cloud", "coral",
	"crane", "crest", "crown", "dune", "echo",
	"ember", "falcon", "fern", "finch", "flint",
	"forge", "frost", "glade", "grove", "harbor",
	"haven", "hawk", "hazel", "heron", "holly",
	"inlet", "iris", "jade", "larch", "lark",
	"lotus", "maple", "marsh", "mesa", "mist",
	"oak", "olive", "onyx", "orbit", "otter",
	"palm", "peak", "pine", "plume", "pond",
	"quail", "quartz", "raven", "reef", "ridge",
	"river", "robin", "sage", "shore", "slate",
	"spark", "spruce", "stone", "surge", "swift",
	"thorn", "tide", "trail", "vale", "vane",
	"vine", "wren", "yarrow", "zenith", "zephyr",
}

func GenerateFunName() string {
	a := adjectives[rand.Intn(len(adjectives))]
	n := nouns[rand.Intn(len(nouns))]
	return fmt.Sprintf("%s-%s", a, n)
}
