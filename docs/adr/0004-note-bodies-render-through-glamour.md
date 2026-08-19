# Note bodies render through glamour

A note's markdown is rendered by [glamour](https://github.com/charmbracelet/glamour) rather
than shown as its own source, as SPEC §6 asks ("glamour-style plain rendering; keep it
simple"). The renderer is built once at startup and its output is cached on the body text.

## The cost

Glamour brings goldmark, chroma, and bluemonday with it. The `trig` binary goes from 5.8 MB
to 14.2 MB — a 2.4× increase, most of it chroma's lexers and themes, none of which a card
this narrow will ever show off. This is the single largest dependency in the project and it
exists to format at most ten lines of text inside an 18-column box.

It is accepted because the alternative is worse in the place it matters: a note is written
in markdown, and a card showing `- item` and `**bold**` as literal punctuation is a card
showing the user their source rather than their writing. The binary is downloaded once; the
card is read every time the map is opened. Revisit at M5 if release size becomes a
constraint — dropping to a hand-rolled renderer for bullets and headings alone would recover
most of the size and lose the long tail of markdown.

## Two things the rendering had to be careful about

**The style is chosen before Bubble Tea takes the terminal.** Picking dark or light asks the
terminal what colour it is, and that question is a write to stdout and a read of the reply.
Asked mid-render it would paint over the map and its answer would arrive as keystrokes, so
`New` builds the renderer while Trigpoint still owns the terminal to ask with. Glamour's own
`WithAutoStyle` would have asked lazily, on the first note drawn.

**The renderer is not the trust boundary.** Glamour passes text it does not understand
straight through, so a pasted escape sequence would reach the card and repaint the map from
inside it. Control characters are stripped from the body before it is parsed, not after it
is rendered.

## The cache

`bodyHeight` asks every node on the map how tall it wants to be on every frame, so an
uncached render would put a markdown parse per note into every keystroke. Measured on this
machine: 44 µs to render a short note, 22 ns to fetch one from the cache — a map of fifty
notes would otherwise spend 2.2 ms per frame in goldmark alone.

The cache is keyed on body text and emptied wholesale when it outgrows 256 entries, which is
generous next to any real map. An LRU would be the upgrade if that ever shows.
