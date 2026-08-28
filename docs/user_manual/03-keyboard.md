# 3. Keyboard shortcuts

Every key Wake binds, and nothing else. The *legend under the composer* is enforced: a test parses
the key handler and the hint line and requires an exact match both ways, so a key with no label and
a label with no key are each a build failure. **This page is not in that loop** — it drifted behind
the legend twice before anyone noticed — so what it promises is weaker and worth stating plainly:
every key here is one the library names and the terminal sends. Whether your particular terminal
sends it is [chapter 7](07-troubleshooting.md) and `docs/live-testing.md`.

## Talking

| Key | Does |
|---|---|
| `↵` | Send |
| `⎋` | Interrupt the current turn — safe on a blocked agent, no respawn |
| `⌥↑` `⌥↓` | Walk back through what you typed into **this** pane, and forward again. `⌥↓` past the newest gives you back the draft you were writing |
| `⌥↵` `⌃J` | A newline in the draft, rather than sending it |
| `⎋⎋` | Clear a conversation's draft. The first `⎋` interrupts *and* arms; the second clears |
| `⇞` `⇟` | Scroll the pane with the keys |
| `⌃E` | Expand what the pane folded — a conversation's tool results, or the room's folded responses — and collapse it again |

A long tool result is cut to its first ten lines with a `… +N lines` footer, so an agent that read a
300-line file shows you thirty of them. `⌃E` shows the whole of every result in that conversation,
and a second press puts them back. It acts on the **pane with the keys** rather than the roster's
pick, because expanding is about what you are reading.

**In the room** `⌃E` does the same for the responses the room folds into a `⤷ … ⌃D open DM` pointer:
it opens every folded response at once, and a second press folds them back. A long response is a
summary in the room by design — the group chat is a hub, not a place for deep reading — so the
default stays folded and `⌃E` is the way to see them all in full without leaving the room. To open
just one, **click** its folded pointer.

Expanding returns you to the newest line. That is the same thing a width change does, for the same
reason: the lines a scroll position points at have renumbered underneath it.

`↑↓` are Claude Code's prompt-history keys and Wake's roster keys, and Wake's do not move — so the
history is on the same two arrows with `⌥` held. A conversation's history is the conversation:
Wake keeps no prompt file, it reads the user turns already in the pane, and a pane filled from
claude's own transcript has a history the first time you open it. The room's history is what you
typed into the room, mention and all.

## Moving between things

| Key | Does |
|---|---|
| `⇥` | Move the keys between the room and the conversations open beside it |
| `⇧⇥` | Cycle the permission mode of the agent the roster has selected: `default` → `acceptEdits` → `plan` → `auto`, the same four Claude Code walks |
| `⌃X` | Jump to the next blocked agent |
| `↑↓` | Move the roster cursor — which is what the three open keys below read |
| `⌃D` | Open the selected agent **into the focused pane** |
| `⌃Y` | Open the selected agent in a **new column** |
| `⌃B` | Open the selected agent **below** the focused pane |
| `⌃W` | Close the open conversation |
| `⌃G` | Toggle the workspaces sidebar |
| `⌃R` | Toggle the activity sidebar |
| `⇧←` `⇧→` `⇧↑` `⇧↓` | Move the keys to the pane that way. It moves among panes **already drawn** and opens nothing — that is the whole difference from `⇥`, and why a direction with no pane in it names `⇥` instead of wrapping |
| `⌃N` `⌃P` | Walk the **dispatch list** — the subagents the focused conversation has spawned. While a completion menu is open they walk that instead |

`⌃N` and `⌃P` are the conversation's own list, not the fleet's. An agent that spawns subagents gets a
row per dispatch inside its pane; these walk them and **`↵` opens the one the cursor is on**. With
nothing selected there, `↵` opens the agent the *roster* cursor is on instead — the dispatch list is
checked first, because its cursor is only ever set by somebody walking it while the roster's is set
by anything that opens the sidebar. Between two cursors, the one you just moved is the one you meant.

**A completion menu takes these keys while it is up**, the way it takes `⇥`. So a draft mid-`@` or
mid-`/` puts the dispatch list out of reach until the menu is gone — one more character, or a key
that is not one of the three. Nobody ruled on that: the completion menu and the dispatch list were
built in parallel and both wanted the pair, and the order they intercept in decided it.

## Lifecycle — read [chapter 4](04-lifecycle.md) first

| Key | Does | Reversible |
|---|---|---|
| `⌃C` | **Park** the agent in front of you | Yes — `/resume` |
| `⌃Q` | **Quit** Wake and park the whole fleet | Yes — next `wake` offers it back |
| `⌃O` | **Arm a detach** — closing Wake and leaving everyone working. `↵` finishes it; a second `⌃O` takes it back. While it is armed the legend reads `↵ detach   …   ⌃O cancel` | Nothing stopped |

## While the completion menu is up — see [chapter 6](06-commands.md)

These three exist only while the word **your cursor is on** has started a `/command` or an `@`, which
is why they are not in the legend under the composer. The menu names them itself, on the menu.

| Key | Does |
|---|---|
| `⇥` | Take the highlighted completion — otherwise `⇥` moves the keys between panes as usual |
| `⌃N` `⌃P` | Move down and up the list |

`↑↓` and `↵` are **not** among them: the arrows stay the roster's, and enter stays send.

Move the cursor off that word — or type a space — and all three go back to the text area, where `⌃N`
and `⌃P` are how you move between the lines of a multi-line draft.

## Other

| Key | Does |
|---|---|
| `⌃F` | Fork the conversation with the keys — see [chapter 5](05-forking.md) |
| `⌃T` | Flip the mention mode — whether `@name …` routes to that agent or is read as text |

## The mouse

Wheel scrolls the pane you are over. Clicking a pane gives it the keys. The divider between the
room and a conversation can be grabbed and dragged. Clicking a folded block opens just that one — a
tool result or a run's rollup in a conversation, or a folded response in the room — where `⌃E`
opens all of them.

Mouse reporting is on, which may mean your terminal needs a modifier (often `⌥` or `⇧`) to select
text for copying.

## Keys that are deliberately absent

This project removes a shortcut rather than let it lie, so these are worth knowing:

**`⌃⇧A` is bound to nothing.** It was the intended key for next-blocked. The terminal library Wake
uses produces no key event for it at all — under either the Kitty keyboard protocol or xterm's
`modifyOtherKeys` — so enabling a protocol would only make the chord silently vanish. `⌃↵` is the
same.

**No key is a `⌃⇧`+arrow, and that is macOS's doing rather than the library's.** The grid keys
shipped as `⌃⇧→` and `⌃⇧↓`, which every terminal sends and the library names — and which no Mac
ever delivered, because the window server spends all four on spaces and Mission Control before a
terminal sees one. `⌃Y` and `⌃B` are single bytes for that reason. If you would rather have the
arrows back, they are macOS shortcuts 79–82 and 32–35 in `com.apple.symbolichotkeys`, not something
Wake can take.

**There is no key to create an agent.** `⌃D`, `⌃Y` and `⌃B` all open the agent the roster cursor is
on; none of them makes one. Every session starts from the shell.

**Prompt history is not on `⌥`+a letter, and that was measured rather than preferred.** `⌥P` and its
neighbours are delivered by a terminal with no keyboard protocol and by nothing under one — both the
Kitty and `modifyOtherKeys` encodings vanish — so a key on one of them would stop working the day a
terminal got better. `⌥`+arrow survives both.

## Keys you bring from Claude Code

Wake runs Claude Code sessions, so you arrive with its keyboard in your hands. Several chords mean
something else here, and `internal/ui/keymap_test.go` holds the list against the bindings read out
of the shipped `claude` binary — one nobody has ruled on is a build failure rather than a surprise.

| Chord | Claude Code | Wake |
|---|---|---|
| `⌃O` | Expand the tool result it just truncated | **Detach.** The only one that costs anything, so it is armed: `⌃O` then `↵`. Pressing `⌃O` again cancels, which is what makes key repeat harmless. `⌃E` expands here |
| `⌃T` | Raise the todo panel | Flip the mention mode — the line above the keys says which reading is live, and the same key puts it back |
| `⌃G` | Open the draft in `$EDITOR` | Toggle the workspaces sidebar |
| `⌃R` | Search your prompt history | Toggle the activity sidebar. There is no search here; `⌥↑↓` walks without one |
| `⌃B` | Background the running task | Open the picked agent below the focused pane — `⌃W` closes it |
| `⌃E` | Show the whole transcript / edit a custom theme / expand a confirmation's explanation | Expand this conversation's tool results. The first of those is the same meaning on both sides, reached by accident |
| `⇧⇥` | Cycle the permission mode | Cycle the permission mode of the agent the roster has selected. The closest thing here to an alignment |
| `⌃N` `⌃P` | Walk its own footer list | Walk the dispatch list, or the completion menu while one is open — **the same job on both sides.** Claude Code binds `up`/`⌃P` and `down`/`⌃N` for it; Wake can have the second pair and not the first, because `↑↓` are the roster's and open the sidebar as they move |

## Shadowed keys

`⌃D`, `⌃W`, `⌃F`, `⌃B` and `⌃E` shadow bindings the text input would otherwise have (`⌃F` is "forward
one character" and `⌃B` "back one character", each of which is `→` or `←` by another name; `⌃E` is
"line end", which is `End`). That is a deliberate trade. `⌃Y` shadows nothing — it was the last key
neither the terminal library nor the text input claimed, and the grid spent it.

**`⌃E` is not Claude Code's key for this, and could not be.** Claude Code expands with `⌃O`, which
detaches here — and detach is the whole reason the background daemon exists. Of the bytes the text
input does not already claim, `⌃S` is the terminal's flow control and `⌃Z` its suspend, so the choice
was between shadowing an editing binding and taking a key the terminal eats first. `End` still
reaches line-end, which made `⌃E` the cheapest one to spend.

`⌃C` is worth naming separately: your terminal normally turns it into an interrupt *signal*. Wake
puts the terminal in raw mode, which stops that — so `⌃C` reaches Wake and parks, rather than
killing it. Under tmux or ssh this is the first thing worth checking.

## At narrow widths

The hint line is 303 cells at full width and truncates from the right — at an entry boundary, so you
never see half of one — which means at an ordinary terminal size you see the first several keys.
They are ordered so that the ones you need most survive: send, interrupt, detach, park, then
navigation. Prompt history sits below `↑↓ pick agent`, so a narrow pane loses it.

Note the legend is cut to the width of the **pane**, not the terminal — with a conversation open
beside the room, a 120-column terminal gives each pane about 50, and only four entries fit.
