package theme

// The named palettes.
//
// Every role carries a light and a dark value, so a theme is a whole look on
// either kind of terminal rather than a dark look that happens to be legible
// on white. Semantic colours stay recognisably good/warn/bad in all of them,
// including the near-monochrome one: a dashboard whose job is answering "is
// anything wrong?" cannot trade that away for a colour scheme.
var palettes = map[string]Palette{
	// ctos is the namesake look: cyan instrumentation on cool slate, the
	// reason the project is named after a surveillance system. Held one
	// step back from the neon of the games — the accent is desaturated, the
	// greys carry the cyan cast instead, so the colour reads as a tint on
	// the whole panel rather than as glare on one border.
	"ctos": {
		Name:    "ctos",
		Summary: "muted cyan on cool slate, bracketed frames",

		Accent: Pair{Light: "#0b6b7d", Dark: "#4fb8cc"},

		Faint: Pair{Light: "#a6bcc2", Dark: "#42565e"},
		Dim:   Pair{Light: "#5c757e", Dark: "#7d949c"},
		Text:  Pair{Light: "#12222a", Dark: "#d6e4e8"},

		Good: Pair{Light: "#12794f", Dark: "#57c98a"},
		Warn: Pair{Light: "#8a5a00", Dark: "#e0a34a"},
		Bad:  Pair{Light: "#a8202c", Dark: "#e05f6b"},

		Border: Pair{Light: "#c3d2d6", Dark: "#2f4148"},
		Chrome: Bracketed,
	},

	// dedsec is the loud one: acid lime against magenta, the spray-can half
	// of the same world. Kept as a separate theme rather than a brighter
	// ctos because the contrast is the point of it.
	"dedsec": {
		Name:    "dedsec",
		Summary: "acid lime and magenta, high contrast",

		Accent: Pair{Light: "#3f7a00", Dark: "#9ae64a"},

		Faint: Pair{Light: "#b4b4a8", Dark: "#4c4c44"},
		Dim:   Pair{Light: "#6b6b60", Dark: "#93938a"},
		Text:  Pair{Light: "#161616", Dark: "#e8e8e0"},

		Good: Pair{Light: "#1f7a52", Dark: "#5fd38d"},
		Warn: Pair{Light: "#8a5a00", Dark: "#e8c34a"},
		Bad:  Pair{Light: "#a01050", Dark: "#f0509a"},

		Border: Pair{Light: "#cfcfc4", Dark: "#3a3a33"},
		Chrome: Bracketed,
	},

	// blume is the corporate counterpart: clinical blue and white, rounded
	// corners, nothing shouting. The theme to run when the dashboard is on
	// a screen someone else can see.
	"blume": {
		Name:    "blume",
		Summary: "clinical corporate blue",

		Accent: Pair{Light: "#0a5bd0", Dark: "#5a9ef5"},

		Faint: Pair{Light: "#b2b9c6", Dark: "#474e5c"},
		Dim:   Pair{Light: "#5e6675", Dark: "#8b93a3"},
		Text:  Pair{Light: "#14181f", Dark: "#dfe4ec"},

		Good: Pair{Light: "#0a7a4a", Dark: "#3fc98a"},
		Warn: Pair{Light: "#8a6100", Dark: "#e0ad3f"},
		Bad:  Pair{Light: "#c02030", Dark: "#f0616f"},

		Border: Pair{Light: "#ccd3de", Dark: "#333a47"},
		Chrome: Rounded,
	},

	// ember is what ctOS looked like before it had themes, and stays the
	// default: an upgrade should not repaint a dashboard somebody has
	// already settled into.
	"ember": {
		Name:    "ember",
		Summary: "orange on neutral grey — the default",

		Accent: Pair{Light: "#ff6b35", Dark: "#ff6b35"},

		Faint: Pair{Light: "#b0b0b0", Dark: "#4a4a4a"},
		Dim:   Pair{Light: "#777777", Dark: "#8a8a8a"},
		Text:  Pair{Light: "#1a1a1a", Dark: "#e4e4e4"},

		Good: Pair{Light: "#1a7f37", Dark: "#3fb950"},
		Warn: Pair{Light: "#9a6700", Dark: "#d29922"},
		Bad:  Pair{Light: "#cf222e", Dark: "#f85149"},

		Border: Pair{Light: "#d0d0d0", Dark: "#3a3a3a"},
		Chrome: Rounded,
	},

	// noir is the quiet one: greys everywhere, the accent a plain contrast
	// against the background. Its semantic colours are desaturated rather
	// than removed, so a failing disk is still the only coloured thing on
	// the screen — which is more or less the point.
	"noir": {
		Name:    "noir",
		Summary: "near-monochrome; colour only where something is wrong",

		Accent: Pair{Light: "#111111", Dark: "#f0f0f0"},

		Faint: Pair{Light: "#b4b4b4", Dark: "#4f4f4f"},
		Dim:   Pair{Light: "#6e6e6e", Dark: "#8a8a8a"},
		Text:  Pair{Light: "#1a1a1a", Dark: "#d4d4d4"},

		Good: Pair{Light: "#4a6b52", Dark: "#8fae96"},
		Warn: Pair{Light: "#7a6a45", Dark: "#c2ac78"},
		Bad:  Pair{Light: "#8a4a4a", Dark: "#cc8a8a"},

		Border: Pair{Light: "#d4d4d4", Dark: "#3a3a3a"},
		Chrome: Rounded,
	},

	// ---------------------------------------------------------------
	// Ports of published colour schemes.
	//
	// Colours transcribed from each project's own specification, with
	// credit and thanks:
	//
	//   dracula      Dracula, Zeno Rocha — draculatheme.com (MIT)
	//                light half is Alucard, its official light variant
	//   nord         Nord, Arctic Ice Studio — nordtheme.com (MIT)
	//   gruvbox      gruvbox, Pavel Pertsev — github.com/morhetz/gruvbox (MIT)
	//   catppuccin   Catppuccin — catppuccin.com (MIT); Mocha and Latte
	//   tokyonight   Tokyo Night, enkia — github.com/enkia/tokyo-night-vscode-theme (MIT)
	//                light half is Tokyo Night Day
	//   solarized    Solarized, Ethan Schoonover — ethanschoonover.com/solarized (MIT)
	//   rosepine     Rosé Pine — rosepinetheme.com (MIT); Main and Dawn
	//   onedark      One Dark, Atom — github.com/atom/atom (MIT); with One Light
	//
	// Each one's own light variant is the light half of the Pair, which is
	// what makes these ports rather than dark schemes with a guessed light
	// mode. Nord is the exception: it publishes no light variant, so its
	// light half is its Polar Night greys used as ink on Snow Storm, which
	// is what its own documentation suggests.
	//
	// Roles are mapped, not invented: a scheme's comment grey becomes Dim,
	// its selection or gutter grey becomes Faint, and its red/yellow/green
	// become Bad/Warn/Good. Where a scheme names a default accent — mauve
	// in Catppuccin, iris in Rosé Pine — that is the accent here too.
	// ---------------------------------------------------------------

	"dracula": {
		Name:    "dracula",
		Ported:  true,
		Summary: "purple and pink on charcoal",

		Accent: Pair{Light: "#644ac9", Dark: "#bd93f9"},

		Faint: Pair{Light: "#b8b3d6", Dark: "#44475a"},
		Dim:   Pair{Light: "#635d97", Dark: "#6272a4"},
		Text:  Pair{Light: "#1f1f1f", Dark: "#f8f8f2"},

		Good: Pair{Light: "#14710a", Dark: "#50fa7b"},
		Warn: Pair{Light: "#a34d14", Dark: "#ffb86c"},
		Bad:  Pair{Light: "#cb3a2a", Dark: "#ff5555"},

		Border: Pair{Light: "#d5d0e8", Dark: "#363948"},
		Chrome: Rounded,
	},

	"nord": {
		Name:    "nord",
		Ported:  true,
		Summary: "arctic blue on polar night",

		Accent: Pair{Light: "#5e81ac", Dark: "#88c0d0"},

		Faint: Pair{Light: "#c2cad6", Dark: "#4c566a"},
		Dim:   Pair{Light: "#4c566a", Dark: "#7b88a1"},
		Text:  Pair{Light: "#2e3440", Dark: "#e5e9f0"},

		Good: Pair{Light: "#6a7f4f", Dark: "#a3be8c"},
		Warn: Pair{Light: "#8a6d2f", Dark: "#ebcb8b"},
		Bad:  Pair{Light: "#a3454f", Dark: "#bf616a"},

		Border: Pair{Light: "#d8dee9", Dark: "#3b4252"},
		Chrome: Rounded,
	},

	"gruvbox": {
		Name:    "gruvbox",
		Ported:  true,
		Summary: "retro warmth; gold on brown",

		Accent: Pair{Light: "#b57614", Dark: "#fabd2f"},

		Faint: Pair{Light: "#d5c4a1", Dark: "#665c54"},
		Dim:   Pair{Light: "#7c6f64", Dark: "#a89984"},
		Text:  Pair{Light: "#3c3836", Dark: "#ebdbb2"},

		Good: Pair{Light: "#79740e", Dark: "#b8bb26"},
		Warn: Pair{Light: "#af3a03", Dark: "#fe8019"},
		Bad:  Pair{Light: "#9d0006", Dark: "#fb4934"},

		Border: Pair{Light: "#ebdbb2", Dark: "#3c3836"},
		Chrome: Bracketed,
	},

	"catppuccin": {
		Name:    "catppuccin",
		Ported:  true,
		Summary: "pastel mauve on soft dark (Mocha / Latte)",

		Accent: Pair{Light: "#8839ef", Dark: "#cba6f7"},

		Faint: Pair{Light: "#acb0be", Dark: "#585b70"},
		Dim:   Pair{Light: "#6c6f85", Dark: "#9399b2"},
		Text:  Pair{Light: "#4c4f69", Dark: "#cdd6f4"},

		Good: Pair{Light: "#40a02b", Dark: "#a6e3a1"},
		Warn: Pair{Light: "#df8e1d", Dark: "#f9e2af"},
		Bad:  Pair{Light: "#d20f39", Dark: "#f38ba8"},

		Border: Pair{Light: "#ccd0da", Dark: "#313244"},
		Chrome: Rounded,
	},

	"tokyonight": {
		Name:    "tokyonight",
		Ported:  true,
		Summary: "neon blue on deep navy",

		Accent: Pair{Light: "#2e7de9", Dark: "#7aa2f7"},

		Faint: Pair{Light: "#a8aecb", Dark: "#414868"},
		Dim:   Pair{Light: "#6172b0", Dark: "#787c99"},
		Text:  Pair{Light: "#343b58", Dark: "#c0caf5"},

		Good: Pair{Light: "#587539", Dark: "#9ece6a"},
		Warn: Pair{Light: "#8c6c3e", Dark: "#e0af68"},
		Bad:  Pair{Light: "#f52a65", Dark: "#f7768e"},

		Border: Pair{Light: "#c4c8da", Dark: "#292e42"},
		Chrome: Rounded,
	},

	// solarized is the one scheme whose accents are deliberately the same
	// on both backgrounds — only the base greys flip. Written that way here
	// rather than "corrected", because that symmetry is the whole design.
	"solarized": {
		Name:    "solarized",
		Ported:  true,
		Summary: "the classic; identical accents on either background",

		Accent: Pair{Light: "#268bd2", Dark: "#268bd2"},

		Faint: Pair{Light: "#93a1a1", Dark: "#586e75"},
		Dim:   Pair{Light: "#657b83", Dark: "#839496"},
		Text:  Pair{Light: "#586e75", Dark: "#93a1a1"},

		Good: Pair{Light: "#859900", Dark: "#859900"},
		Warn: Pair{Light: "#b58900", Dark: "#b58900"},
		Bad:  Pair{Light: "#dc322f", Dark: "#dc322f"},

		Border: Pair{Light: "#eee8d5", Dark: "#073642"},
		Chrome: Bracketed,
	},

	"rosepine": {
		Name:    "rosepine",
		Ported:  true,
		Summary: "muted iris and gold (Main / Dawn)",

		Accent: Pair{Light: "#907aa9", Dark: "#c4a7e7"},

		Faint: Pair{Light: "#b9b0bd", Dark: "#6e6a86"},
		Dim:   Pair{Light: "#797593", Dark: "#908caa"},
		Text:  Pair{Light: "#575279", Dark: "#e0def4"},

		// Rosé Pine has no green: foam, its cool accent, is what the
		// scheme's own editors use for a healthy or added state.
		Good: Pair{Light: "#56949f", Dark: "#9ccfd8"},
		Warn: Pair{Light: "#ea9d34", Dark: "#f6c177"},
		Bad:  Pair{Light: "#b4637a", Dark: "#eb6f92"},

		Border: Pair{Light: "#dfd8d3", Dark: "#26233a"},
		Chrome: Rounded,
	},

	"onedark": {
		Name:    "onedark",
		Ported:  true,
		Summary: "the Atom editor blue (One Dark / One Light)",

		Accent: Pair{Light: "#4078f2", Dark: "#61afef"},

		Faint: Pair{Light: "#b6b8bf", Dark: "#4b5263"},
		Dim:   Pair{Light: "#696c77", Dark: "#7f848e"},
		Text:  Pair{Light: "#383a42", Dark: "#abb2bf"},

		Good: Pair{Light: "#50a14f", Dark: "#98c379"},
		Warn: Pair{Light: "#c18401", Dark: "#e5c07b"},
		Bad:  Pair{Light: "#e45649", Dark: "#e06c75"},

		Border: Pair{Light: "#d3d5db", Dark: "#3e4451"},
		Chrome: Rounded,
	},
}
