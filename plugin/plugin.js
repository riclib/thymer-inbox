/**
 * Thymer Paste Plugin
 *
 * Polls a Cloudflare Worker queue for content and inserts it into Thymer.
 * Supports: markdown paste, lifelog entries, record creation.
 *
 * Configure THYMER_QUEUE_URL and THYMER_QUEUE_TOKEN in plugin settings.
 */

// Defaults for local development (matches `tm serve` defaults)
const DEFAULT_QUEUE_URL = 'http://localhost:19501';
const DEFAULT_QUEUE_TOKEN = 'local-dev-token';

// Valid highlight languages for code blocks
const VALID_LANGUAGES = new Set([
    'bash', 'c', 'coffeescript', 'cpp', 'csharp', 'css', 'dart', 'diff',
    'go', 'graphql', 'ini', 'java', 'javascript', 'json', 'kotlin', 'less',
    'lua', 'makefile', 'markdown', 'objectivec', 'perl', 'php', 'php-template',
    'plaintext', 'powershell', 'python', 'python-repl', 'r', 'ruby', 'rust',
    'scala', 'scss', 'shell', 'sql', 'swift', 'typescript', 'vbnet', 'wasm',
    'xml', 'yaml'
]);

// Language aliases mapping
const LANGUAGE_ALIASES = {
    'js': 'javascript',
    'ts': 'typescript',
    'py': 'python',
    'rb': 'ruby',
    'sh': 'bash',
    'zsh': 'bash',
    'yml': 'yaml',
    'objective-c': 'objectivec',
    'objc': 'objectivec',
    'c++': 'cpp',
    'c#': 'csharp',
    'cs': 'csharp',
    'golang': 'go',
    'rs': 'rust',
    'kt': 'kotlin',
    'md': 'markdown',
    'html': 'xml',
    'htm': 'xml'
};

function normalizeLanguage(lang) {
    if (!lang) return null;
    const lower = lang.toLowerCase().trim();
    if (VALID_LANGUAGES.has(lower)) return lower;
    if (LANGUAGE_ALIASES[lower]) return LANGUAGE_ALIASES[lower];
    return null; // Unknown language, will default to plaintext
}

class Plugin extends AppPlugin {

    onLoad() {
        this.connected = false;
        this.eventSource = null;

        // Get config from plugin settings (set in plugin.json or via API)
        const config = this.getExistingCodeAndConfig?.()?.json || {};
        this.queueUrl = config.queueUrl || DEFAULT_QUEUE_URL;
        this.queueToken = config.queueToken || DEFAULT_QUEUE_TOKEN;

        // Status bar - click to retry connection
        this.statusBarItem = this.ui.addStatusBarItem({
            htmlLabel: '<span style="font-size: 14px;">🪄</span> <span style="opacity: 0.5;">...</span>',
            tooltip: 'Thymer Paste - Connecting...',
            onClick: () => this.retryConnection()
        });

        // Command palette: Paste Markdown
        this.pasteCommand = this.ui.addCommandPaletteCommand({
            label: 'Paste Markdown',
            icon: 'clipboard-text',
            onSelected: () => this.pasteMarkdownFromClipboard()
        });

        // Command palette: Dump Line Items (for debugging)
        this.dumpCommand = this.ui.addCommandPaletteCommand({
            label: 'Dump Line Items',
            icon: 'bug',
            onSelected: () => this.dumpLineItems()
        });

        // Command palette: Sync Readwise
        this.readwiseSyncCommand = this.ui.addCommandPaletteCommand({
            label: 'Sync Readwise',
            icon: 'books',
            onSelected: () => this.triggerReadwiseSync()
        });

        // Auto-connect on load
        this.startStream();
    }

    onUnload() {
        if (this.statusBarItem) {
            this.statusBarItem.remove();
        }
        if (this.pasteCommand) {
            this.pasteCommand.remove();
        }
        if (this.dumpCommand) {
            this.dumpCommand.remove();
        }
        if (this.readwiseSyncCommand) {
            this.readwiseSyncCommand.remove();
        }
        this.stopStream();
    }

    retryConnection() {
        this.stopStream();
        this.statusBarItem.setHtmlLabel('<span style="font-size: 14px;">🪄</span> <span style="opacity: 0.5;">...</span>');
        this.statusBarItem.setTooltip('Thymer Paste - Connecting...');
        this.startStream();
    }

    startStream() {
        // Build URL with token as query param (EventSource can't set headers)
        const streamUrl = `${this.queueUrl}/stream` +
            (this.queueToken ? `?token=${this.queueToken}` : '');

        this.eventSource = new EventSource(streamUrl);

        this.eventSource.onopen = () => {
            this.setConnected(true);
        };

        this.eventSource.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                if (data.content || data.markdown) {
                    this.handleQueueItem(data);
                }
            } catch (e) {
                console.error('Failed to parse SSE message:', e);
            }
        };

        this.eventSource.addEventListener('connected', () => {
            this.setConnected(true);
        });

        this.eventSource.addEventListener('error', () => {
            // EventSource auto-reconnects, just update status
            this.setConnected(false);
        });

        this.eventSource.onerror = () => {
            this.setConnected(false);
        };
    }

    stopStream() {
        if (this.eventSource) {
            this.eventSource.close();
            this.eventSource = null;
        }
    }

    async pasteMarkdownFromClipboard() {
        try {
            const markdown = await navigator.clipboard.readText();
            if (!markdown || !markdown.trim()) {
                this.ui.addToaster({
                    title: '🪄 Paste Markdown',
                    message: 'Clipboard is empty',
                    dismissible: true,
                    autoDestroyTime: 2000,
                });
                return;
            }
            await this.insertMarkdown(markdown);
        } catch (error) {
            this.ui.addToaster({
                title: '🪄 Paste Markdown',
                message: `Failed to read clipboard: ${error.message}`,
                dismissible: true,
                autoDestroyTime: 3000,
            });
        }
    }

    async triggerReadwiseSync() {
        try {
            const response = await fetch(`${this.queueUrl}/readwise-sync`, {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${this.queueToken}`
                }
            });
            if (response.ok) {
                this.ui.addToaster({
                    title: '📚 Readwise',
                    message: 'Sync triggered',
                    dismissible: true,
                    autoDestroyTime: 2000,
                });
            } else {
                const text = await response.text();
                this.ui.addToaster({
                    title: '📚 Readwise',
                    message: `Sync failed: ${text}`,
                    dismissible: true,
                    autoDestroyTime: 3000,
                });
            }
        } catch (error) {
            this.ui.addToaster({
                title: '📚 Readwise',
                message: `Sync failed: ${error.message}`,
                dismissible: true,
                autoDestroyTime: 3000,
            });
        }
    }

    async dumpLineItems() {
        const panel = this.ui.getActivePanel();
        const record = panel?.getActiveRecord();
        const collection = panel?.getActiveCollection();

        console.log('=== CONTEXT ===');
        console.log('panel:', panel?.getId());
        console.log('collection:', collection?.getName(), '| guid:', collection?.getGuid());
        console.log('record:', record?.getName(), '| guid:', record?.guid);

        // Dump all collections
        console.log('\n=== ALL COLLECTIONS ===');
        const collections = await this.data.getAllCollections();
        for (const c of collections) {
            console.log(`- ${c.getName()} | guid: ${c.getGuid()}`);
        }

        if (!record) {
            console.log('\nNo active record - open a note to see line items');
            return;
        }

        // Dump record properties
        console.log('\n=== RECORD PROPERTIES ===');
        const props = record.getAllProperties();
        for (const prop of props) {
            console.log(`- ${prop.name}: text=${prop.text()} | number=${prop.number()} | date=${prop.date()}`);
        }

        // Dump line items (first 10)
        const lineItems = await record.getLineItems();
        console.log(`\n=== LINE ITEMS (${lineItems.length} total, showing first 10) ===`);
        for (const item of lineItems.slice(0, 10)) {
            const segmentParts = item.segments?.map(s => {
                if (s.type === 'text') return s.text;
                if (s.type === 'ref') return `[ref:${s.text?.guid || '?'}]`;
                if (typeof s.text === 'object') return `[${s.type}:${JSON.stringify(s.text)}]`;
                return `[${s.type}:${s.text || ''}]`;
            }) || [];
            const text = segmentParts.join('');
            console.log(`- type: ${item.type} | "${text.slice(0, 50)}..." | guid: ${item.guid} | parent: ${item.parent_guid}`);
            if (item._item?.mp) console.log('  mp:', JSON.stringify(item._item.mp));
        }
        console.log('=== END ===');

        this.ui.addToaster({
            title: '🪄 Dump',
            message: `Logged ${collections.length} collections, ${props.length} props, ${lineItems.length} items`,
            dismissible: true,
            autoDestroyTime: 3000,
        });
    }

    async handleQueueItem(data) {
        const rawContent = data.content || data.markdown || '';
        const action = data.action || 'append';
        const cliTimestamp = data.createdAt ? new Date(data.createdAt) : new Date();
        const timeStr = cliTimestamp.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });

        // Check for frontmatter - universal interface for all integrations
        const { meta, body } = this.parseFrontmatter(rawContent);
        const hasFrontmatter = Object.keys(meta).length > 0;
        const content = hasFrontmatter ? body : rawContent;

        // If frontmatter specifies a collection, route there
        if (hasFrontmatter && meta.collection) {
            await this.handleFrontmatterItem(data.title || meta.title, meta, body);
            return;
        }

        // If CLI passed --collection flag, route there (non-frontmatter content)
        if (data.collection) {
            const syntheticMeta = { collection: data.collection };
            await this.handleFrontmatterItem(data.title, syntheticMeta, content);
            return;
        }

        // Find today's Journal entry
        const journalRecord = await this.getTodayJournalRecord();

        if (!journalRecord) {
            this.ui.addToaster({
                title: '🪄 Error',
                message: 'Could not find today\'s Journal entry',
                dismissible: true,
                autoDestroyTime: 3000,
            });
            return;
        }

        // Handle lifelog action specially
        if (action === 'lifelog') {
            await this.insertMarkdown(`**${timeStr}** ${content}`, journalRecord);
            this.ui.addToaster({
                title: '🪄 Lifelog',
                message: `${timeStr} ${content.slice(0, 40)}${content.length > 40 ? '...' : ''}`,
                dismissible: true,
                autoDestroyTime: 2000,
            });
            return;
        }

        // Detect content type
        const lines = content.split('\n').filter(l => l.trim() !== '');
        const isOneLiner = lines.length === 1;
        const isShort = lines.length >= 2 && lines.length <= 5 && !content.trim().startsWith('# ');
        const isMarkdownDoc = content.trim().startsWith('# ');

        if (isOneLiner) {
            // One-liner: simple append with timestamp
            await this.appendOneLiner(journalRecord, timeStr, content.trim());
            this.ui.addToaster({
                title: '🪄 Quick note',
                message: content.slice(0, 50),
                dismissible: true,
                autoDestroyTime: 2000,
            });
        } else if (isShort) {
            // Short content (2-5 lines): first line as parent, rest as children
            await this.appendShortNote(journalRecord, timeStr, lines);
            this.ui.addToaster({
                title: '🪄 Note added',
                message: `${lines.length} lines to Journal`,
                dismissible: true,
                autoDestroyTime: 2000,
            });
        } else if (isMarkdownDoc) {
            // Markdown document: create in Inbox, add ref to Journal
            const result = await this.createInboxNote(content, cliTimestamp);
            if (result) {
                await this.addSyncRefToJournal(journalRecord, timeStr, 'added', result.guid);
                this.ui.addToaster({
                    title: '🪄 Note created',
                    message: `"${result.title}" in Inbox`,
                    dismissible: true,
                    autoDestroyTime: 2000,
                });
            } else {
                // Fallback: insert as markdown in Journal
                await this.insertMarkdown(content, journalRecord);
                this.ui.addToaster({
                    title: '🪄 Added to Journal',
                    message: 'Could not create Inbox note, added to Journal instead',
                    dismissible: true,
                    autoDestroyTime: 3000,
                });
            }
        } else {
            // Default (>5 lines, no heading): use hierarchy, add timestamp to first line
            const firstLine = lines[0];
            const restLines = content.split('\n').slice(1).join('\n');
            const timestampedContent = `**${timeStr}** ${firstLine}\n${restLines}`;
            await this.insertMarkdown(timestampedContent, journalRecord);
            this.ui.addToaster({
                title: '🪄 Content added',
                message: `${lines.length} lines to Journal`,
                dismissible: true,
                autoDestroyTime: 2000,
            });
        }
    }

    async appendOneLiner(record, timeStr, text) {
        // Simple one-liner: "15:21 Quick thought"
        const existingItems = await record.getLineItems();
        const topLevelItems = existingItems.filter(item => item.parent_guid === record.guid);
        const lastItem = topLevelItems.length > 0 ? topLevelItems[topLevelItems.length - 1] : null;

        const newItem = await record.createLineItem(null, lastItem, 'text');
        if (newItem) {
            newItem.setSegments([
                { type: 'bold', text: timeStr },
                { type: 'text', text: ' ' + text }
            ]);
        }
    }

    async appendShortNote(record, timeStr, lines) {
        // Short note (2-5 lines): first line as parent with timestamp, rest as children
        const existingItems = await record.getLineItems();
        const topLevelItems = existingItems.filter(item => item.parent_guid === record.guid);
        const lastItem = topLevelItems.length > 0 ? topLevelItems[topLevelItems.length - 1] : null;

        // Create parent item with first line
        const parentItem = await record.createLineItem(null, lastItem, 'text');
        if (!parentItem) return;

        parentItem.setSegments([
            { type: 'bold', text: timeStr },
            { type: 'text', text: ' ' + lines[0] }
        ]);

        // Parse remaining content with full markdown support, insert under parent
        const restContent = lines.slice(1).join('\n');
        if (restContent.trim()) {
            await this.insertMarkdown(restContent, record, parentItem);
        }
    }

    async createInboxNote(content, timestamp) {
        try {
            // Find Inbox collection (or fallback to Notes)
            const collections = await this.data.getAllCollections();
            let inbox = collections.find(c => c.getName() === 'Inbox');
            if (!inbox) {
                inbox = collections.find(c => c.getName() === 'Notes');
            }
            if (!inbox) {
                console.error('Neither Inbox nor Notes collection found');
                return null;
            }

            // Extract title from first heading
            const titleMatch = content.match(/^#\s+(.+)$/m);
            const title = titleMatch ? titleMatch[1].trim() : 'Untitled';

            // Create the record
            const newRecordGuid = inbox.createRecord(title);
            if (!newRecordGuid) {
                console.error('Failed to create record in Inbox');
                return null;
            }

            // Get the record from collection (might need a moment to sync)
            await new Promise(resolve => setTimeout(resolve, 100)); // Brief delay for sync
            const records = await inbox.getAllRecords();
            const newRecord = records.find(r => r.guid === newRecordGuid);
            if (!newRecord) {
                console.error('Could not get newly created record:', newRecordGuid);
                // Still return success - record was created, just couldn't add body
                return { guid: newRecordGuid, title };
            }

            // Remove the first heading line from content (it's the title now)
            const bodyContent = content.replace(/^#\s+.+\n?/, '').trim();
            if (bodyContent) {
                await this.insertMarkdown(bodyContent, newRecord);
            }

            return { guid: newRecordGuid, title };
        } catch (e) {
            console.error('Error creating Inbox note:', e);
            return null;
        }
    }

    async addSyncRefToJournal(journalRecord, timeStr, action, guid) {
        // Add: "15:21 added [[Title]]" or "15:21 updated [[Title]]"
        const existingItems = await journalRecord.getLineItems();
        const topLevelItems = existingItems.filter(item => item.parent_guid === journalRecord.guid);
        const lastItem = topLevelItems.length > 0 ? topLevelItems[topLevelItems.length - 1] : null;

        const newItem = await journalRecord.createLineItem(null, lastItem, 'text');
        if (newItem) {
            newItem.setSegments([
                { type: 'bold', text: timeStr },
                { type: 'text', text: ` ${action} ` },
                { type: 'ref', text: { guid } }
            ]);
        }
    }

    async clearAndReplaceContent(record, newContent) {
        // Clear existing line items and replace with new content
        try {
            const existingItems = await record.getLineItems();

            // Delete all existing line items
            for (const item of existingItems) {
                try {
                    await item.delete();
                } catch (e) {
                    // Some items might not be deletable, continue
                }
            }

            // Small delay for sync
            await new Promise(resolve => setTimeout(resolve, 100));

            // Insert new content
            await this.insertMarkdown(newContent, record);
        } catch (e) {
            console.error('Error replacing content:', e);
            // Fallback: just append
            await this.insertMarkdown(newContent, record);
        }
    }

    parseFrontmatter(content) {
        // Parse YAML frontmatter from markdown
        const match = content.match(/^---\n([\s\S]*?)\n---\n\n?([\s\S]*)$/);
        if (!match) return { meta: {}, body: content };

        const yamlStr = match[1];
        const body = match[2];
        const meta = {};

        // Simple YAML parsing (key: value)
        for (const line of yamlStr.split('\n')) {
            const colonIdx = line.indexOf(':');
            if (colonIdx > 0) {
                const key = line.slice(0, colonIdx).trim();
                let value = line.slice(colonIdx + 1).trim();
                // Handle arrays like [a, b, c]
                if (value.startsWith('[') && value.endsWith(']')) {
                    value = value.slice(1, -1).split(',').map(s => s.trim());
                }
                // Handle booleans
                else if (value === 'true') value = true;
                else if (value === 'false') value = false;
                // Handle numbers
                else if (/^\d+$/.test(value)) value = parseInt(value, 10);
                meta[key] = value;
            }
        }

        return { meta, body };
    }

    async handleFrontmatterItem(title, meta, body) {
        // Universal handler for frontmatter-based content
        // Routes to collection, finds existing by external_id, adds journal entries
        const collectionName = meta.collection;
        const externalId = meta.external_id;
        const verb = meta.verb; // e.g., opened, closed, merged, updated

        // Find target collection
        const collections = await this.data.getAllCollections();
        const targetCollection = collections.find(c =>
            c.getName().toLowerCase() === collectionName.toLowerCase()
        );

        if (!targetCollection) {
            console.error('Collection not found:', collectionName);
            this.ui.addToaster({
                title: '🪄 Error',
                message: `Collection "${collectionName}" not found`,
                dismissible: true,
                autoDestroyTime: 3000,
            });
            return;
        }

        // Find existing record by external_id only
        // - If external_id provided: search for match, update if found
        // - If no external_id: always create new record (e.g., manual `cat foo.md | tm`)
        // No title fallback - causes issues like recurring calendar events collapsing
        const records = await targetCollection.getAllRecords();
        let existingRecord = null;

        if (externalId) {
            for (const record of records) {
                try {
                    const extIdProp = record.prop('external_id');
                    const storedExtId = extIdProp?.text();
                    if (extIdProp && storedExtId === externalId) {
                        existingRecord = record;
                        break;
                    }
                } catch (e) {
                    // Skip records without external_id field
                }
            }
        }

        const timeStr = new Date().toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
        const journalRecord = await this.getTodayJournalRecord();

        if (existingRecord) {
            // Update existing - set properties
            await this.setPropertiesFromMeta(existingRecord, meta);

            // Update body content - clear existing and re-insert
            if (body.trim()) {
                await this.clearAndReplaceContent(existingRecord, body);
            }

            // Only add to journal if verb is specified (silent update otherwise)
            if (journalRecord && verb) {
                await this.addSyncRefToJournal(journalRecord, timeStr, verb, existingRecord.guid);
            }

            this.ui.addToaster({
                title: `📦 ${this.capitalize(verb || 'synced')}`,
                message: title,
                dismissible: true,
                autoDestroyTime: 2000,
            });
        } else {
            // Create new record
            const newGuid = targetCollection.createRecord(title);
            if (!newGuid) {
                console.error('Failed to create record');
                return;
            }

            // Default verb to "captured" for manual captures (no external_id, no verb, has content)
            const effectiveVerb = verb || (!externalId && body.trim() ? 'captured' : null);

            // Add to journal if we have a verb
            if (journalRecord && effectiveVerb) {
                await this.addSyncRefToJournal(journalRecord, timeStr, effectiveVerb, newGuid);
            }

            // Wait for sync and get record to set properties
            await new Promise(resolve => setTimeout(resolve, 100));
            const allRecords = await targetCollection.getAllRecords();
            const newRecord = allRecords.find(r => r.guid === newGuid);

            if (newRecord) {
                await this.setPropertiesFromMeta(newRecord, meta);
                if (body.trim()) {
                    await this.insertMarkdown(body, newRecord);
                }
            }

            this.ui.addToaster({
                title: `📦 ${this.capitalize(effectiveVerb || 'synced')}`,
                message: title,
                dismissible: true,
                autoDestroyTime: 2000,
            });
        }
    }

    capitalize(str) {
        return str.charAt(0).toUpperCase() + str.slice(1);
    }

    async setPropertiesFromMeta(record, meta) {
        // Try to set any property that matches a meta key
        // This is generic - works with any Collection Plugin fields
        const skipKeys = ['collection', 'title']; // These are routing, not properties

        for (const [key, value] of Object.entries(meta)) {
            if (skipKeys.includes(key)) continue;

            try {
                const prop = record.prop(key);
                if (!prop) continue;

                // Skip datetime fields here - handled separately with range support
                if ((key === 'start' || key === 'end') && typeof value === 'number') {
                    continue;
                }

                if (typeof value === 'boolean' || typeof value === 'number') {
                    prop.set(value);
                } else if (typeof value === 'string') {
                    // Try setChoice first for choice fields (matches by label)
                    if (typeof prop.setChoice === 'function') {
                        const success = prop.setChoice(value);
                        if (!success) {
                            // Fall back to regular set for text fields
                            prop.set(value);
                        }
                    } else {
                        prop.set(value);
                    }
                } else if (Array.isArray(value)) {
                    // Could be multi-select or tags
                    prop.set(value.join(', '));
                }
            } catch (e) {
                // Property doesn't exist on this collection, skip silently
            }
        }

        // Handle datetime range (start/end) using Thymer's DateTime class
        await this.setDateTimeRange(record, meta);
    }

    async setDateTimeRange(record, meta) {
        const startEpoch = meta.start;
        const endEpoch = meta.end;
        const allDay = meta.all_day === true;

        if (typeof startEpoch !== 'number') return;

        // Check if DateTime class is available
        const DateTimeClass = (typeof DateTime !== 'undefined') ? DateTime : null;

        if (!DateTimeClass) {
            console.log('[tm] DATETIME: DateTime class not yet available - skipping time range');
            return;
        }

        try {
            const startDate = new Date(startEpoch * 1000);
            const startDt = new DateTimeClass(startDate);

            // For all-day events, strip the time component
            if (allDay) {
                startDt.setTime(null);
            }

            // If we have an end time, create a range
            if (typeof endEpoch === 'number') {
                let endDate = new Date(endEpoch * 1000);

                if (allDay) {
                    // Google Calendar uses exclusive end dates for all-day events
                    // Dec 27 all-day → start=Dec 27, end=Dec 28 (exclusive)
                    // Subtract 1 day to make it inclusive
                    endDate = new Date(endDate.getTime() - 24 * 60 * 60 * 1000);

                    // If start == adjusted end, it's a single day - no range needed
                    if (startDate.toDateString() === endDate.toDateString()) {
                        console.log(`[tm] DATETIME all-day single: ${startDate.toDateString()}`);
                        // Don't set range, just use start
                    } else {
                        // Multi-day all-day event
                        const endDt = new DateTimeClass(endDate);
                        endDt.setTime(null);
                        startDt.setRangeTo(endDt);
                        console.log(`[tm] DATETIME all-day range: ${startDate.toDateString()} → ${endDate.toDateString()}`);
                    }
                } else {
                    // Regular timed event - create range with both times
                    const endDt = new DateTimeClass(endDate);
                    startDt.setRangeTo(endDt);
                    console.log(`[tm] DATETIME range: ${startDate.toISOString()} → ${endDate.toISOString()}`);
                }
            } else {
                console.log(`[tm] DATETIME single: ${startDate.toISOString()} (all_day: ${allDay})`);
            }

            // Set the range on the 'time_period' property (single field holds full range)
            const timeProp = record.prop('time_period');
            if (timeProp) {
                const val = startDt.value();
                console.log('[tm] DATETIME value:', JSON.stringify(val));
                timeProp.set(val);
                console.log('[tm] DATETIME set complete, prop.date()=', timeProp.date?.());
            }
        } catch (e) {
            console.error('[tm] DATETIME range failed:', e);
        }
    }

    isISODateTime(str) {
        // Check if string matches ISO datetime format (e.g., "2025-12-27T10:00:00+02:00")
        return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/.test(str);
    }

    async getTodayJournalRecord() {
        try {
            const collections = await this.data.getAllCollections();
            const journal = collections.find(c => c.getName() === 'Journal');
            if (!journal) {
                console.error('Journal collection not found');
                return null;
            }

            const records = await journal.getAllRecords();
            const today = new Date().toISOString().slice(0, 10).replace(/-/g, ''); // "20251226"
            const todayRecord = records.find(r => r.guid.endsWith(today));

            if (!todayRecord) {
                console.error('Today\'s Journal entry not found, looking for:', today);
                return null;
            }

            return todayRecord;
        } catch (e) {
            console.error('Error finding Journal:', e);
            return null;
        }
    }

    setConnected(connected) {
        if (this.connected !== connected) {
            this.connected = connected;

            if (connected) {
                this.statusBarItem.setHtmlLabel('<span style="font-size: 14px;">🪄</span> <span style="color: #4ade80;">●</span>');
                this.statusBarItem.setTooltip('Thymer Paste - Connected');
            } else {
                this.statusBarItem.setHtmlLabel('<span style="font-size: 14px;">🪄</span> <span style="color: #f87171;">●</span>');
                this.statusBarItem.setTooltip('Thymer Paste - Disconnected (click to retry)');
            }
        }
    }

    async insertMarkdown(markdown, targetRecord = null, parentItem = null) {
        const record = targetRecord || this.ui.getActivePanel()?.getActiveRecord();

        if (!record) {
            this.ui.addToaster({
                title: '🪄 Thymer Paste',
                message: 'No target record. Please open a note first.',
                dismissible: true,
                autoDestroyTime: 5000,
            });
            return;
        }

        // Parse markdown into blocks (handles multi-line code blocks)
        const blocks = this.parseMarkdown(markdown);

        // Find the last item to append after
        // If parentItem provided, we're nesting under it; otherwise at record top level
        const existingItems = await record.getLineItems();
        const containerGuid = parentItem ? parentItem.guid : record.guid;
        const siblingItems = existingItems.filter(item => item.parent_guid === containerGuid);
        let lastItem = siblingItems.length > 0 ? siblingItems[siblingItems.length - 1] : null;

        // Hierarchical nesting based on heading levels
        // Track parent stack: index 0 = root (parentItem or record), index N = heading level N
        // Content goes under the most recent heading
        let parentStack = [{ item: parentItem, afterItem: lastItem, level: 0 }]; // level 0 = root
        let isFirstBlock = true;

        for (let i = 0; i < blocks.length; i++) {
            const block = blocks[i];
            const isHeading = block.type === 'heading';
            const headingLevel = isHeading ? (block.mp?.hsize || 1) : 0;

            try {
                let newItem;
                if (isHeading) {
                    // Pop stack back to parent level (find where this heading belongs)
                    // A heading goes under the nearest heading with a smaller level
                    while (parentStack.length > 1 && parentStack[parentStack.length - 1].level >= headingLevel) {
                        parentStack.pop();
                    }

                    const parent = parentStack[parentStack.length - 1];

                    // Add blank line before headings (except first block)
                    if (!isFirstBlock) {
                        const blankItem = await record.createLineItem(
                            parent.item,  // null for record root, or parentItem
                            parent.afterItem,
                            'text'
                        );
                        if (blankItem) {
                            blankItem.setSegments([]);  // Empty text = blank line
                            parent.afterItem = blankItem;
                        }
                    }
                    newItem = await record.createLineItem(
                        parent.item,  // null for record root, or parentItem
                        parent.afterItem,
                        block.type
                    );

                    if (newItem) {
                        // Update the parent's afterItem for siblings
                        parent.afterItem = newItem;
                        // Set heading size using new API (when available)
                        if (headingLevel > 1 && typeof newItem.setHeadingSize === 'function') {
                            try {
                                newItem.setHeadingSize(headingLevel);
                            } catch (e) {
                                // setHeadingSize not available, continue
                            }
                        }
                        // Set segments and sync (setSegments persists the mp/heading size)
                        newItem.setSegments(block.segments || []);
                        // Push this heading as new parent for deeper content
                        parentStack.push({ item: newItem, afterItem: null, level: headingLevel });
                    }
                } else {
                    // Non-heading content: goes under the most recent heading
                    const parent = parentStack[parentStack.length - 1];
                    newItem = await record.createLineItem(
                        parent.item,  // null for record root, or parentItem
                        parent.afterItem,
                        block.type
                    );

                    if (newItem) {
                        parent.afterItem = newItem;
                    }
                }

                if (newItem) {
                    // Set mp BEFORE setSegments - maybe setSegments syncs everything
                    if (block.mp) {
                        newItem._item.mp = block.mp;
                    }

                    // For code blocks, create child text items for each line
                    if (block.type === 'block' && block.codeLines) {
                        // Set syntax highlighting language using new API (when available)
                        const lang = normalizeLanguage(block.mp?.language);
                        if (lang && typeof newItem.setHighlightLanguage === 'function') {
                            try {
                                newItem.setHighlightLanguage(lang);
                            } catch (e) {
                                // setHighlightLanguage not available, continue
                            }
                        }
                        // Call setSegments on code block to sync mp (required to persist language)
                        newItem.setSegments([]);

                        let codeLastChild = null;
                        for (const line of block.codeLines) {
                            const childItem = await record.createLineItem(newItem, codeLastChild, 'text');
                            if (childItem) {
                                // Use setSegments API
                                childItem.setSegments([{ type: 'text', text: line }]);
                                codeLastChild = childItem;
                            }
                        }
                    } else if (!isHeading && block.segments && block.segments.length > 0) {
                        // Regular items (not headings - they're handled above): use setSegments API
                        newItem.setSegments(block.segments);
                    } else if (!isHeading && block.mp) {
                        // Item has mp but no segments - call setSegments to sync
                        newItem.setSegments([]);
                    }

                    isFirstBlock = false;
                }
            } catch (e) {
                console.error('Failed to create line item:', e);
            }
        }

        if (blocks.length > 0) {
            this.ui.addToaster({
                title: '🪄 Content inserted',
                message: `Added to "${record.getName()}"`,
                dismissible: true,
                autoDestroyTime: 2000,
            });
        }
    }

    parseMarkdown(markdown) {
        const lines = markdown.split('\n');
        const blocks = [];
        let inCodeBlock = false;
        let codeLines = [];
        let codeLanguage = '';

        for (let i = 0; i < lines.length; i++) {
            const line = lines[i];

            // Check for code block start/end
            if (line.startsWith('```')) {
                if (!inCodeBlock) {
                    // Starting a code block
                    inCodeBlock = true;
                    codeLanguage = line.slice(3).trim();
                    codeLines = [];
                } else {
                    // Ending a code block - use 'block' type with child lines
                    inCodeBlock = false;
                    if (codeLines.length > 0) {
                        blocks.push({
                            type: 'block',
                            mp: { language: codeLanguage || 'plaintext' },
                            codeLines: codeLines
                        });
                    }
                    codeLines = [];
                    codeLanguage = '';
                }
                continue;
            }

            if (inCodeBlock) {
                codeLines.push(line);
                continue;
            }

            // Parse regular line
            const parsed = this.parseLine(line);
            if (parsed) {
                blocks.push(parsed);
            }
        }

        // Handle unclosed code block
        if (inCodeBlock && codeLines.length > 0) {
            blocks.push({
                type: 'block',
                mp: { language: codeLanguage || 'plaintext' },
                codeLines: codeLines
            });
        }

        return blocks;
    }

    parseLine(line) {
        // Skip empty lines for now
        if (!line.trim()) {
            return null;
        }

        // Horizontal rule (---, ***, ___, or with spaces)
        if (/^(\*\s*\*\s*\*|\-\s*\-\s*\-|_\s*_\s*_)[\s\*\-_]*$/.test(line.trim())) {
            return {
                type: 'br',
                segments: []
            };
        }

        // Headings
        const headingMatch = line.match(/^(#{1,6})\s+(.+)$/);
        if (headingMatch) {
            const level = headingMatch[1].length; // 1-6 based on # count
            return {
                type: 'heading',
                mp: { hsize: level },
                segments: this.parseInlineFormatting(headingMatch[2])
            };
        }

        // Task list (check before unordered list)
        const taskMatch = line.match(/^[\-\*]\s+\[([ xX])\]\s+(.+)$/);
        if (taskMatch) {
            return {
                type: 'task',
                segments: this.parseInlineFormatting(taskMatch[2])
            };
        }

        // Unordered list
        const ulMatch = line.match(/^[\-\*]\s+(.+)$/);
        if (ulMatch) {
            return {
                type: 'ulist',
                segments: this.parseInlineFormatting(ulMatch[1])
            };
        }

        // Ordered list
        const olMatch = line.match(/^\d+\.\s+(.+)$/);
        if (olMatch) {
            return {
                type: 'olist',
                segments: this.parseInlineFormatting(olMatch[1])
            };
        }

        // Quote
        if (line.startsWith('> ')) {
            return {
                type: 'quote',
                segments: this.parseInlineFormatting(line.slice(2))
            };
        }

        // Regular text
        return {
            type: 'text',
            segments: this.parseInlineFormatting(line)
        };
    }

    parseInlineFormatting(text) {
        const segments = [];

        // Regex patterns for inline formatting
        // Order matters: check longer/more specific patterns first
        const patterns = [
            // Inline code: `code`
            { regex: /`([^`]+)`/, type: 'code' },
            // Links: [text](url)
            { regex: /\[([^\]]+)\]\(([^)]+)\)/, type: 'link' },
            // Bold: **text** or __text__
            { regex: /\*\*([^*]+)\*\*/, type: 'bold' },
            { regex: /__([^_]+)__/, type: 'bold' },
            // Italic: *text* or _text_ (but not inside words for _)
            { regex: /\*([^*]+)\*/, type: 'italic' },
            { regex: /(?:^|[^a-zA-Z])_([^_]+)_(?:$|[^a-zA-Z])/, type: 'italic' },
        ];

        let remaining = text;

        while (remaining.length > 0) {
            let earliestMatch = null;
            let earliestIndex = remaining.length;
            let matchedPattern = null;

            // Find the earliest match among all patterns
            for (const pattern of patterns) {
                const match = remaining.match(pattern.regex);
                if (match && match.index < earliestIndex) {
                    earliestMatch = match;
                    earliestIndex = match.index;
                    matchedPattern = pattern;
                }
            }

            if (earliestMatch && matchedPattern) {
                // Add text before the match
                if (earliestIndex > 0) {
                    segments.push({ type: 'text', text: remaining.slice(0, earliestIndex) });
                }

                // Add the formatted segment
                if (matchedPattern.type === 'link') {
                    // For links, show display text (URL handling TBD)
                    segments.push({
                        type: 'text',
                        text: earliestMatch[1]
                    });
                } else {
                    segments.push({
                        type: matchedPattern.type,
                        text: earliestMatch[1]
                    });
                }

                // Continue with remaining text
                remaining = remaining.slice(earliestIndex + earliestMatch[0].length);
            } else {
                // No more matches, add remaining text
                segments.push({ type: 'text', text: remaining });
                break;
            }
        }

        return segments.length ? segments : [{ type: 'text', text }];
    }
}
