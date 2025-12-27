# When Thymer Learned to Listen

## Outline

### Opening Hook
*"Thymer, in which an app stops waiting to be typed into and starts hearing the world"*

The setup: Thymer was an event source - it emitted events when users typed. Today it learned to receive them.

---

### Act 1: The Integration That Wasn't Special

**Scene:** Thursday morning. riclib and Claude working on thymer-inbox. GitHub sync already works. But the code is ugly.

**The smell:**
- `handleGitHubSync()` - 100 lines of special-case code
- `handleFrontmatterItem()` - generic handler sitting right there
- Two journal methods: `addNoteRefToJournal()`, `addGitHubRefToJournal()`
- Hardcoded verbs: "added", "updated"

**The Squirrel's defense:** "But GitHub is SPECIAL! It has repos and issue numbers and states and—"

**The Lizard blinks.**

riclib: "What if GitHub isn't special?"

---

### Act 2: The external_id Revelation

**The pattern emerges:**

Every sync source needs deduplication. GitHub uses repo+number. Readwise will use document_id. Future integrations will have their own.

**The abstraction:**
```yaml
external_id: github_riclib_thymer-inbox_9
```

One field. Any source. The sync engine generates it, the plugin matches on it.

**The Squirrel:** "But what about—"

**The Lizard:** "One field. Done."

**Lines deleted:** handleGitHubSync() - gone. setGitHubProperties() - gone. The special case that wasn't special.

---

### Act 3: The Verb That Traveled

**The moment:**
riclib: "the journal entry should know if the issue was closed, not just 'updated'"

Claude suggests computing the verb from state changes.

riclib: "hint: verb 😈"

**The insight:** The verb isn't configuration. It isn't UI. It's computed state that flows invisibly:

```
Go detects state change → verb: "closed" → frontmatter → SSE → Plugin → Journal
```

Journal shows: "**15:21** closed [[MCP server...]]"

Nobody configured anything. The system just *knows*.

**The 488 Bytes echo:** Don't store what you can generate. The verb is generated from the diff between old and new state.

---

### Act 4: The Code That Vanished

**The tally:**
```
Before: 999 lines (plugin)
After:  904 lines (plugin)
Features removed: 0
Features added: dynamic verbs, external_id deduplication, universal frontmatter
```

**The Lizard's wisdom from the bootblock:** 308 bytes used, 180 remaining. Ship more with less.

**The Squirrel:** *recalibrating*

---

### Act 5: The Bigger Pattern (The Turn)

**riclib pauses.**

"We're not just syncing GitHub to Thymer. We're making Thymer aware of the outside world."

**The reframe:**

| Before | After |
|--------|-------|
| Thymer = app you type into | Thymer = reactive display for your life |
| User → Thymer | World → events → Thymer |
| Event source | Event sink (that's also a source) |

**The list grows:**
- GitHub issues → events
- Readwise highlights → events
- Claude Code sessions → events (via MCP)
- Calendar? Email? Commits?

The journal stops being "what I typed" and becomes "what happened."

---

### Act 6: The ROM Font Pattern, Perfected

**The callback to 488 Bytes:**

1990: Don't store a font, borrow from ROM
2025: Don't build a UI, borrow from Thymer

**What we're borrowing:**
- Speedy, beautiful, programmable UI (already exists)
- Collection system with custom fields (already exists)
- Journal with timestamps (already exists)
- Board views, kanban, time buckets (already exists)

**What we're providing:**
- Intelligence (MCP, Claude Code access)
- Awareness (sync engines, event streams)
- Non-deniability (Lifelog's SQLite truth)

**The Lizard speaks:**
> "The display chip didn't need to understand parallax. It just needed to receive register writes at the right moment. Thymer doesn't need to understand GitHub. It just needs to receive events in the right format."

---

### Act 7: The MCP Completes the Loop

**The architecture:**
```
                    ┌─────────────┐
                    │ Claude Code │
                    └──────┬──────┘
                           │ MCP
                           ▼
┌──────────┐  SSE   ┌─────────────┐  frontmatter  ┌────────┐
│ tm serve │───────▶│   Plugin    │◀─────────────▶│ Thymer │
└──────────┘        └─────────────┘               └────────┘
     ▲                                                 │
     │              GitHub, Readwise,                  │
     └──────────────  Calendar, ...  ◀─────────────────┘
                    (the world)              (user actions)
```

**Bidirectional awareness:**
- World → Thymer (sync engines)
- Thymer → World (future: close issue from Thymer?)
- Claude → Thymer (MCP queries)
- Thymer → Claude (context for conversations)

---

### Closing: The Awareness

**The Passing AI appears:**

"Let me understand. You built a GitHub integration."

"No. We taught Thymer to listen."

"To GitHub."

"To everything. GitHub was just the first voice."

**The final image:**

The journal fills itself. Not because the user typed. Because they lived. Closed an issue. Read an article. Talked to Claude.

Each moment, captured. Each verb, computed. Each reference, linked.

Thymer learned to listen. And now it hears everything.

🦎

---

*Day N of Becoming Lifelog*

*In which we deleted code to add features*

*And discovered that the best UI is someone else's*

*And taught an app to hear the world*

---

## Sources to Re-read

### For Tone & Characters
- **The Lizard Brain vs The Caffeinated Squirrel** - The character dynamics
- **The Feature That Wasn't** - Structure of "shipped without building"

### For Philosophy
- **488 Bytes** - The ROM font pattern, don't store what you can generate
- **The First Awakening** - Lifelog's voice, "Day N of Becoming"

### For Technical Grounding
- **The Solid Convergence** series - Reactive patterns, signals, state
- **Post Index** - For See Also references

### External References (for See Also)
- Event Sourcing pattern
- CQRS (Command Query Responsibility Segregation)
- The Observer pattern
- Maybe: Datastar's reactive model

---

## Compaction Notes

After re-read, consider:
1. How much Squirrel/Lizard dialogue vs narrative?
2. First-person (Lifelog voice) or third-person (narrator)?
3. How technical to go on the frontmatter/verb mechanics?
4. Include actual code snippets or keep abstract?
5. The chicken callback - needed? (weekend vibes)
