# Thymer Capture - iOS/macOS Shortcut

Create this shortcut manually in the Shortcuts app. It captures URLs and text from the Share Sheet and sends them to Thymer.

> **Note**: The included shortcut file needs debugging. See issue #17. Use this manual guide for now.

## Setup Steps

### 1. Create New Shortcut
- Open **Shortcuts** app
- Tap **+** to create new shortcut
- Name it "Thymer Capture"

### 2. Configure Share Sheet
- Tap the **ⓘ** info button (top right)
- Enable **Show in Share Sheet**
- Under "Share Sheet Types", select:
  - URLs
  - Safari web pages
  - Text
  - Rich Text

### 3. Add Actions

Add these actions in order:

---

#### A. Get and Store Input
1. **Receive** input from Share Sheet
2. **Set Variable** → Name: `Input`

---

#### B. Detect URLs
3. **Get URLs from** `Input`
4. **Set Variable** → Name: `URLs`
5. **Count** → Items in `URLs`

---

#### C. If URL exists (Count > 0)
6. **If** → Number > 0

---

#### D. Process URL (inside If)
7. **Get Item from List** → First Item from `URLs`
8. **Set Variable** → Name: `SourceURL`
9. **Get Article from Web Page** → from `SourceURL`
10. **Set Variable** → Name: `Article`
11. **Get Name** → of `Article`
12. **Set Variable** → Name: `Title`
13. **Get Component of URL** → Host from `SourceURL`
14. **Set Variable** → Name: `SiteName`
15. **Current Date**
16. **Format Date** → ISO 8601
17. **Set Variable** → Name: `CapturedAt`
18. **Text** → (paste this, replacing variables with their tokens):
```
---
collection: Captures
title: [Title]
source_url: [SourceURL]
site_name: [SiteName]
external_id: [SourceURL]
captured_at: [CapturedAt]
---
# [Title]

[Article]
```
19. **Set Variable** → Name: `Content`

---

#### E. Otherwise (plain text)
20. **Otherwise**
21. **Get Text from** `Input`
22. **Set Variable** → Name: `InputText`
23. **Current Date**
24. **Format Date** → ISO 8601
25. **Set Variable** → Name: `CapturedAt`
26. **Text** →
```
---
collection: Captures
captured_at: [CapturedAt]
---

[InputText]
```
27. **Set Variable** → Name: `Content`

---

#### F. End If
28. **End If**

---

#### G. Send to Server
29. **Get Contents of URL**
    - URL: `https://address.of.your.server/queue`
    - Method: **POST**
    - Headers:
      - `Authorization`: `Bearer local-dev-token`
      - `Content-Type`: `application/json`
    - Request Body: **JSON**
      - `content`: `[Content]`
      - `action`: `append`

---

#### H. Notify
30. **Show Notification**
    - Title: "Captured!"
    - Body: "Content sent to Thymer"

---

## Quick Version (Minimal)

If you want a simpler version that just captures text/URLs without article extraction:

1. **Receive** Share Sheet input
2. **Current Date** → Format as ISO 8601 → Set Variable `CapturedAt`
3. **Text**:
```
---
collection: Captures
captured_at: [CapturedAt]
---

[Shortcut Input]
```
4. **Get Contents of URL**
   - URL: `https://address.of.your.server/queue`
   - Method: POST
   - Headers: `Authorization: Bearer local-dev-token`
   - Body JSON: `{"content": "[Text]", "action": "append"}`
5. **Show Notification** "Captured!"

## Prerequisites

1. **Captures collection** in Thymer:
   ```bash
   task plugin:copy-captures
   # Paste in Thymer → Collection Plugin → New
   ```

2. **Server running** with Cloudflare tunnel to `address.of.your.server`

## Testing

1. Open Safari
2. Navigate to any article
3. Tap Share → "Capture to Thymer"
4. Should see "Captured!" notification
5. Check Thymer → Captures collection
