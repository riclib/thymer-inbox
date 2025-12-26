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

class Plugin extends AppPlugin {

    onLoad() {
        this.bridgeEnabled = false;
        this.connected = false;
        this.eventSource = null;

        // Get config from plugin settings (set in plugin.json or via API)
        const config = this.getExistingCodeAndConfig?.()?.json || {};
        this.queueUrl = config.queueUrl || DEFAULT_QUEUE_URL;
        this.queueToken = config.queueToken || DEFAULT_QUEUE_TOKEN;

        // Status bar - click to toggle bridge
        this.statusBarItem = this.ui.addStatusBarItem({
            htmlLabel: '<span style="font-size: 14px;">🪄</span> paste',
            tooltip: 'Thymer Paste - Click to enable queue polling',
            onClick: () => this.toggleBridge()
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
        this.stopStream();
    }

    toggleBridge() {
        if (this.bridgeEnabled) {
            this.stopStream();
            this.bridgeEnabled = false;
            this.connected = false;
            this.statusBarItem.setHtmlLabel('<span style="font-size: 14px;">🪄</span> skill');
            this.statusBarItem.setTooltip('Thymer Paste - Click to enable CLI bridge');
            this.ui.addToaster({
                title: '🪄 Thymer Paste',
                message: 'Bridge disabled',
                dismissible: true,
                autoDestroyTime: 1500,
            });
        } else {
            this.bridgeEnabled = true;
            this.failureCount = 0;
            this.statusBarItem.setHtmlLabel('<span style="font-size: 14px;">🪄</span> <span style="opacity: 0.5;">connecting...</span>');
            this.statusBarItem.setTooltip('Thymer Paste - Connecting...');
            this.startStream();
            this.ui.addToaster({
                title: '🪄 Thymer Paste',
                message: 'Bridge enabled - listening for CLI input',
                dismissible: true,
                autoDestroyTime: 1500,
            });
        }
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
        const content = data.content || data.markdown || '';
        const action = data.action || 'append';

        // Find today's Journal entry
        const targetRecord = await this.getTodayJournalRecord();

        if (!targetRecord) {
            this.ui.addToaster({
                title: '🪄 Error',
                message: 'Could not find today\'s Journal entry',
                dismissible: true,
                autoDestroyTime: 3000,
            });
            return;
        }

        switch (action) {
            case 'lifelog':
                // Add timestamped entry
                const time = new Date().toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' });
                await this.insertMarkdown(`**${time}** ${content}`, targetRecord);
                break;

            case 'create':
                // TODO: Create new record in collection
                await this.insertMarkdown(content, targetRecord);
                break;

            case 'append':
            default:
                await this.insertMarkdown(content, targetRecord);
                break;
        }

        this.ui.addToaster({
            title: '🪄 Received',
            message: `Added to ${targetRecord.getName()}: ${content.slice(0, 40)}${content.length > 40 ? '...' : ''}`,
            dismissible: true,
            autoDestroyTime: 2000,
        });
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
        if (!this.bridgeEnabled) return;

        if (this.connected !== connected) {
            this.connected = connected;

            if (connected) {
                this.statusBarItem.setHtmlLabel('<span style="font-size: 14px;">🪄</span> <span style="color: #4ade80;">●</span> skill');
                this.statusBarItem.setTooltip('Thymer Paste - Connected (click to disable)');
            } else {
                this.statusBarItem.setHtmlLabel('<span style="font-size: 14px;">🪄</span> <span style="color: #f87171;">●</span> skill');
                this.statusBarItem.setTooltip('Thymer Paste - Server not found (click to disable)');
            }
        }
    }

    async insertMarkdown(markdown, targetRecord = null) {
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

        // Find the last top-level line item to append after
        // Top-level items have parent_guid = record.guid
        const existingItems = await record.getLineItems();
        const topLevelItems = existingItems.filter(item => item.parent_guid === record.guid);
        let lastItem = topLevelItems.length > 0 ? topLevelItems[topLevelItems.length - 1] : null;
        // Hierarchical nesting based on heading levels
        // Track parent stack: index 0 = top level, index N = heading level N
        // Content goes under the most recent heading
        let parentStack = [{ item: null, afterItem: lastItem, level: 0 }]; // level 0 = root (record)
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
                            parent.level === 0 ? null : parent.item,
                            parent.afterItem,
                            'text'
                        );
                        if (blankItem) {
                            blankItem.setSegments([]);  // Empty text = blank line
                            parent.afterItem = blankItem;
                        }
                    }
                    newItem = await record.createLineItem(
                        parent.level === 0 ? null : parent.item,
                        parent.afterItem,
                        block.type
                    );

                    if (newItem) {
                        // Update the parent's afterItem for siblings
                        parent.afterItem = newItem;
                        // Push this heading as new parent for deeper content
                        parentStack.push({ item: newItem, afterItem: null, level: headingLevel });
                    }
                } else {
                    // Non-heading content: goes under the most recent heading
                    const parent = parentStack[parentStack.length - 1];
                    newItem = await record.createLineItem(
                        parent.level === 0 ? null : parent.item,
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
                        // Call setSegments on block to sync mp
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
                    } else if (block.segments && block.segments.length > 0) {
                        // Regular items: use setSegments API
                        newItem.setSegments(block.segments);
                    } else if (block.mp) {
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
