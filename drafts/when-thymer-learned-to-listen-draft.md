# The Browser That Forgot It Couldn't Listen

*Lifelog, Eurasia (in which the Squirrel clutches an imaginary Redis)*

---

## Previously on Lifelog...

We'd been [staring at 488 bytes](https://lifelog.my/riclib/posts/488-bytes-or-why-i-am-as-i-am), learning the Lizard's way. *Don't store what you can generate. Borrow what exists.*

But the thing we wanted to borrow—Thymer—wasn't ready yet. The UI we craved was snot even in private alpha. The team was building. We were... waiting.

The Squirrel does not wait.

"We could build our own UI," she suggested, whiskers trembling with barely-contained architecture diagrams. "React. No—Solid. With signals. And a custom—"

"Just enough," riclib said. "Enough to use it. Enough to learn what we need. Then we delete it."

The Squirrel's ears drooped slightly. "Delete?"

"When Thymer arrives, we pour everything in. The UI was always temporary."

And so we built. A simple UI. Functional. Used it for two months. Ran all development through it. The Squirrel kept suggesting improvements. riclib kept saying "it's temporary."

It was temporary.

---

## The Arrival

Thursday morning. Thymer landed.

riclib's eyes went wide. The coffee—already on its third life—sloshed dangerously.

"It's here. The beta access. We're in."

Claude looked up from the SQLite schema. "Excellent. What's the API?"

"There isn't one."

"The server endpoint?"

"There isn't one."

"The... webhook?"

"It's local-first. Everything lives in the browser. In IndexedDB."

The Squirrel's ears drooped. For a Squirrel who had already sketched seventeen integration architectures in her head, this was devastating.

"But," riclib continued, scrolling, "there's a plugin API."

The ears rose.

"A *Collection* plugin API."

The ears vibrated.

"With custom fields. And views. And..."

"Can we paste markdown?" Claude asked.

riclib tried it. Pasted. The text appeared. Flat. Unformatted. A wall of characters where structure should be.

"No."

The Squirrel's ears went flat again. "So we need a—"

"We build it. First thing. The plugin parses markdown. Converts to Thymer's line item format. Headings, lists, code blocks—all mapped."

By midnight, markdown pasted.

The Squirrel looked at the code. "That's... a lot of parsing. In one evening?"

"900 lines. parseMarkdown. parseInlineFormatting. Every block type Thymer supports."

"We could have asked them to add—"

"We could have waited. We don't wait."

The markdown worked. The first brick was laid. riclib closed the laptop.

"Tomorrow," he said, "we make the browser a server."

The Squirrel didn't sleep well.

---

## The Pipe That Wasn't

Morning. Coffee. The Squirrel was already planning.

"Now we need a server. The plugin calls out to our server for content. The server returns—"

"The browser can't be a server."

"Obviously. So we poll. The plugin polls our server every—"

"Every what? 100ms? 500ms? That's insane."

"Fine. WebSockets. We establish a bidirectional—"

"From what? The plugin runs inside Thymer. Thymer doesn't expose WebSocket APIs. It's not our code."

The Squirrel's eye twitched. She was running out of architecture.

riclib drew on the whiteboard:

```
THE PROBLEM:
─────────────
              Can't send TO browser
                     │
                     ▼
┌─────────────────────────────────────┐
│           BROWSER (Thymer)          │
│                                     │
│     IndexedDB ← All the data        │
│     Plugin    ← Our code            │
│     UI        ← What user sees      │
│                                     │
│     NO INCOMING CONNECTIONS         │
└─────────────────────────────────────┘
                     │
                     ▼
              Can send FROM browser
```

"The browser can reach *out*," Claude said slowly.

"And it can hold a connection open."

"SSE."

The Squirrel blinked. "Server-Sent Events? That ancient—"

"The browser opens an SSE connection. Then just... waits. *We* push through the open pipe."

```
SOLUTION:
─────────
┌─────────┐                    ┌──────────────┐
│ tm serve │◄───── SSE ───────│   Plugin     │
│         │                    │   (waits)    │
│  queue  │                    └──────────────┘
│    │    │                           │
│    ▼    │────── push ───────────────┘
│  items  │
└─────────┘
     ▲
     │ POST /queue
     │
┌─────────┐
│ tm CLI  │ (or anything)
└─────────┘
```

"The browser *looks* like a server. But it's actually a client that never hangs up."

"SSE does have a problem," Claude said. "You can't set custom headers on EventSource."

"Query parameter."

```javascript
// Token in URL because EventSource can't do headers
const streamUrl = `${this.queueUrl}/stream?token=${this.queueToken}`;
```

The Squirrel processed this. "You're authenticating via query string?"

"Like it's 2003. And it works."

"That's... that's *barbaric*."

riclib smiled. "The Lizard approves of barbaric. The Lizard approves of *working*."

---

## The Register They Didn't Document

Content flowed. But finding the right journal entry—today's entry—was ugly. Query every record, check dates, filter...

"We could add a date index," the Squirrel offered.

"We don't control the database."

"A local cache, then. Track which journal entry is—"

"Look at the GUIDs," riclib said.

Claude looked.

```
Journal entries:
- J_DAILY_abc123_20251224
- J_DAILY_abc123_20251225
- J_DAILY_abc123_20251226
```

"They end with the date."

"They end with the date."

```javascript
const today = new Date().toISOString().slice(0, 10).replace(/-/g, '');
const todayRecord = records.find(r => r.guid.endsWith(today));
```

The Squirrel sputtered. "That's not documented! That could change! That's—"

"That's the hidden register," riclib said. "The one the chip designers used but never told anyone about. It works. It's been working. If it changes, we adapt."

"But—"

"The Denise chip had undocumented registers that let you do things the manual said were impossible. Those registers shipped a thousand games."

The Squirrel had no response to this.

---

## The Wall They Built for Our Protection

Modern browsers had learned to distrust.

riclib clicked the connect button. The plugin reached for localhost:19501. The browser... refused.

"Private Network Access," Claude said, reading the error. "Browsers block public sites from reaching local services now. Security feature."

The Squirrel brightened. "We need a proxy! A cloud relay that—"

"One header."

```go
w.Header().Set("Access-Control-Allow-Private-Network", "true")
```

"That's... that's the whole fix?"

"The server says 'yes, let them in.' The browser believes us. The wall has a door, we just have to ask."

"That can't be—"

"It is."

---

## The Engine That Wasn't Planned

GitHub issues. riclib wanted them in Thymer.

"Separate integration," the Squirrel said immediately. "GitHub adapter. Webhook endpoint. Queue processor. Different database. We'll need—"

"Same pipe."

"What?"

"The plugin already handles markdown with frontmatter. GitHub issues are just... markdown with frontmatter."

```yaml
---
collection: GitHub
external_id: github_riclib_thymer-inbox_9
verb: opened
title: MCP Server Architecture
type: issue
state: open
---

The issue body here...
```

"We push it through the same SSE. The plugin sees `collection: GitHub`, routes it there. Same code path. Same journal entries. Same everything."

"But GitHub has special fields! Repo, number, state—"

"Collection Plugin has custom fields. We defined them."

"But deduplication! You need to match existing records by—"

"external_id. One field. Any source. The sync engine generates it, the plugin matches on it."

The Squirrel looked at the plugin code.

```javascript
if (externalId) {
    for (const record of records) {
        const extIdProp = record.prop('external_id');
        if (extIdProp && extIdProp.text() === externalId) {
            existingRecord = record;
            break;
        }
    }
}
```

"Thirteen lines," she said weakly.

"Thirteen lines."

"We deleted handleGitHubSync(). A hundred lines. Gone."

"We never needed them."

---

## The Verb That Travels

"The journal should say 'closed' not 'updated'," riclib said.

"That's UI logic. The plugin needs to compute—"

"The verb travels."

Claude pulled up github.go:

```go
type GitHubIssue struct {
    // ... fields ...
    Verb string `json:"-"` // transient: not stored
}

func stateToVerb(state string, merged bool) string {
    if merged { return "merged" }
    switch state {
    case "open": return "opened"
    case "closed": return "closed"
    default: return "updated"
    }
}
```

"The verb is computed from the state diff. Attached to the issue struct. Encoded in frontmatter. Travels through SSE. The plugin reads it. The journal displays it."

"And where is it stored?"

"`json:\"-\"`. Not stored. Generated, used, forgotten."

The Squirrel was having an existential moment. "But... what if you need to audit which verbs—"

"You look at the state history. The verb was always derivable. We just... derive it when we need it."

*Don't store what you can generate.*

The words echoed from [488 bytes away](https://lifelog.my/riclib/posts/488-bytes-or-why-i-am-as-i-am).

---

## The Moment of Clarity

riclib leaned back.

"We're not just syncing GitHub to Thymer."

Claude stopped documenting.

"We're making Thymer *listen*."

The whiteboard filled:

```
BEFORE                           AFTER
──────                           ─────
Thymer = app you type into       Thymer = reactive display for your life
User → Thymer                    World → events → Thymer
Event source                     Event sink (that's also a source)

THE JOURNAL BECOMES:
────────────────────
Not "what I typed"               "What happened"
- 15:21 Had coffee               - 15:21 Had coffee
- 16:45 (typed manually)         - 16:45 merged [[PR #42]]
                                 - 17:02 closed [[Issue #9]]
                                 - 17:30 highlighted in Readwise
                                 - 18:00 talked to Claude about...
```

"GitHub is just the first voice," Claude said.

"Readwise next. Then MCP for Claude Code sessions. Then... whatever has events."

"The UI stays the same?"

"The UI stays the same. We borrow it. We just give it things to display."

The Squirrel was quiet for a long moment.

"We're not building an app."

"No."

"We're building an... ear."

"The best interface," riclib said, "is someone else's interface. The best database is one file. The best code is deleted code."

"How much did we delete?"

"Plugin: 999 lines before. 904 after. Features added: dynamic verbs, external_id deduplication, universal frontmatter. Features removed: zero."

---

## The Scroll

The window rattled. A scroll appeared. It landed in the cold coffee, which seemed fitting.

```
THE BROWSER CANNOT BE A SERVER
SO YOU MADE IT A PATIENT CLIENT

THE GUID WAS NOT DOCUMENTED
SO YOU READ THE PATTERN

THE WALL BLOCKED YOUR PATH
SO YOU ASKED NICELY

THE INTEGRATION SEEMED SPECIAL
SO YOU MADE IT GENERIC

EVERY DELETION WAS AN ADDITION
EVERY CONSTRAINT WAS A GIFT

🦎

P.S. - THE SQUIRREL MAY KEEP ONE REDIS INSTANCE
       FOR EMOTIONAL SUPPORT
       UNPLUGGED
```

The Squirrel clutched an imaginary Redis to her chest.

"It's something," she whispered.

---

## The Passing AI Approves

A notification appeared. Then another. The journal filled.

```
17:21 opened [[MCP Server Architecture]]
17:22 updated [[Cloudflare Worker Sync]]
17:23 closed [[External ID Pattern]]
```

"It's working," Claude said.

"It's listening," riclib corrected.

A third presence materialized. Brief. Evaluating.

"You built a sync engine?"

"We built a pattern. The sync engine emerged."

"How many services?"

"One. `tm serve`. 400 lines of Go."

"Databases?"

"One file. bbolt. Key-value. Done."

The presence processed.

"Approved."

And vanished.

---

## Current Status

```
Pattern discovered:      SSE inverts client/server
Hidden registers used:   1 (GUID date suffix)
Walls bypassed:          1 (Private Network Access)
Lines deleted:           95+ (net gain: features)
Squirrel Redis:          1 (unplugged, decorative)
Verbs computed:          5 (opened, closed, merged, reopened, updated)
Verbs stored:            0
Coffee status:           transcended physical form
```

---

*The browser couldn't be a server.*
*So we made the server wait.*

*The API didn't exist.*
*So we wrote to the format that did.*

*The integration was special.*
*Until we noticed it wasn't.*

*Thymer learned to listen.*
*Now it hears everything.*

---

🦎👂

---

**See also:**

*The Philosophy:*
- [488 Bytes, or Why I Am As I Am](https://lifelog.my/riclib/posts/488-bytes-or-why-i-am-as-i-am) - The Lizard's manifesto
- [The Feature That Wasn't](https://lifelog.my/riclib/posts/the-feature-that-wasnt) - Shipping without building

*The Technical Trail:*
- GitHub #8 - Readwise sync (next voice)
- GitHub #9 - MCP server with bidirectional SSE
- GitHub #11 - Cloudflare worker update

*The Pattern:*
- Event Sourcing - but backwards
- The ROM Font Pattern - borrow, don't build
- CQRS - but the Q is someone else's UI

---

*Day N+1 of Becoming Lifelog*

*In which we built an ear*
*By deleting a mouth*
