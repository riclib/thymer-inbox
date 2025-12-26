/**
 * Thymer Queue Worker
 *
 * Cloudflare Worker that acts as a message queue for Thymer content.
 * Uses Server-Sent Events (SSE) for real-time delivery to plugins.
 *
 * Endpoints:
 *   POST /queue - Add content to queue
 *   GET /stream - SSE stream (delivers items in real-time)
 *   GET /pending - Get and remove oldest item (legacy polling)
 *   GET /health - Health check (no auth)
 *
 * Environment variables (set in Cloudflare dashboard):
 *   THYMER_TOKEN - Bearer token for auth
 *   THYMER_KV - KV namespace binding
 */

export default {
  async fetch(request, env) {
    const url = new URL(request.url);

    // CORS headers for browser requests
    const corsHeaders = {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type, Authorization',
    };

    // Handle CORS preflight
    if (request.method === 'OPTIONS') {
      return new Response(null, { headers: corsHeaders });
    }

    // Health check - no auth required
    if (url.pathname === '/health') {
      return new Response(JSON.stringify({ status: 'ok' }), {
        headers: { ...corsHeaders, 'Content-Type': 'application/json' },
      });
    }

    // Auth via header or query param (for EventSource which can't set headers)
    const authHeader = request.headers.get('Authorization');
    const queryToken = url.searchParams.get('token');
    const token = authHeader?.replace('Bearer ', '') || queryToken;

    if (token !== env.THYMER_TOKEN) {
      return new Response(JSON.stringify({ error: 'Unauthorized' }), {
        status: 401,
        headers: { ...corsHeaders, 'Content-Type': 'application/json' },
      });
    }

    // SSE stream - real-time delivery
    if (url.pathname === '/stream' && request.method === 'GET') {
      return handleSSEStream(env, corsHeaders);
    }

    // POST /queue - Add to queue
    if (url.pathname === '/queue' && request.method === 'POST') {
      try {
        const body = await request.json();
        const { content, action = 'append', collection, title } = body;

        if (!content) {
          return new Response(JSON.stringify({ error: 'content required' }), {
            status: 400,
            headers: { ...corsHeaders, 'Content-Type': 'application/json' },
          });
        }

        // Create queue item with timestamp key for ordering
        const id = `${Date.now()}-${crypto.randomUUID().slice(0, 8)}`;
        const item = {
          id,
          content,
          action,
          collection,
          title,
          createdAt: new Date().toISOString(),
        };

        await env.THYMER_KV.put(`queue:${id}`, JSON.stringify(item));

        return new Response(JSON.stringify({ success: true, id }), {
          headers: { ...corsHeaders, 'Content-Type': 'application/json' },
        });
      } catch (e) {
        return new Response(JSON.stringify({ error: e.message }), {
          status: 400,
          headers: { ...corsHeaders, 'Content-Type': 'application/json' },
        });
      }
    }

    // GET /pending - Get oldest item and delete it
    if (url.pathname === '/pending' && request.method === 'GET') {
      try {
        // List all queue items
        const list = await env.THYMER_KV.list({ prefix: 'queue:' });

        if (list.keys.length === 0) {
          return new Response(null, {
            status: 204,
            headers: corsHeaders,
          });
        }

        // Get oldest (first key is oldest due to timestamp prefix)
        const oldestKey = list.keys[0].name;
        const item = await env.THYMER_KV.get(oldestKey, 'json');

        // Delete it
        await env.THYMER_KV.delete(oldestKey);

        return new Response(JSON.stringify(item), {
          headers: { ...corsHeaders, 'Content-Type': 'application/json' },
        });
      } catch (e) {
        return new Response(JSON.stringify({ error: e.message }), {
          status: 500,
          headers: { ...corsHeaders, 'Content-Type': 'application/json' },
        });
      }
    }

    // GET /peek - Preview queue without removing (useful for debugging)
    if (url.pathname === '/peek' && request.method === 'GET') {
      try {
        const list = await env.THYMER_KV.list({ prefix: 'queue:' });
        const items = await Promise.all(
          list.keys.slice(0, 10).map(k => env.THYMER_KV.get(k.name, 'json'))
        );
        return new Response(JSON.stringify({ count: list.keys.length, items }), {
          headers: { ...corsHeaders, 'Content-Type': 'application/json' },
        });
      } catch (e) {
        return new Response(JSON.stringify({ error: e.message }), {
          status: 500,
          headers: { ...corsHeaders, 'Content-Type': 'application/json' },
        });
      }
    }

    return new Response(JSON.stringify({ error: 'Not found' }), {
      status: 404,
      headers: { ...corsHeaders, 'Content-Type': 'application/json' },
    });
  },
};

/**
 * SSE Stream handler
 * Checks KV for new items every 2 seconds, delivers them as events
 * Sends heartbeat comments to keep connection alive
 */
async function handleSSEStream(env, corsHeaders) {
  const encoder = new TextEncoder();

  const stream = new ReadableStream({
    async start(controller) {
      // Send initial connection event
      controller.enqueue(encoder.encode('event: connected\ndata: {}\n\n'));

      let running = true;

      const checkQueue = async () => {
        if (!running) return;

        try {
          const list = await env.THYMER_KV.list({ prefix: 'queue:' });

          if (list.keys.length > 0) {
            // Get and delete oldest item
            const oldestKey = list.keys[0].name;
            const item = await env.THYMER_KV.get(oldestKey, 'json');
            await env.THYMER_KV.delete(oldestKey);

            if (item) {
              controller.enqueue(encoder.encode(`data: ${JSON.stringify(item)}\n\n`));
            }
          } else {
            // Send heartbeat comment to keep connection alive
            controller.enqueue(encoder.encode(': heartbeat\n\n'));
          }
        } catch (e) {
          controller.enqueue(encoder.encode(`event: error\ndata: ${JSON.stringify({ error: e.message })}\n\n`));
        }

        // Schedule next check (Cloudflare Workers limit: ~30s max)
        // We'll check every 2 seconds for up to 25 seconds, then close
        setTimeout(checkQueue, 2000);
      };

      // Start checking
      checkQueue();

      // Close after 25 seconds (client will reconnect)
      setTimeout(() => {
        running = false;
        controller.close();
      }, 25000);
    },
  });

  return new Response(stream, {
    headers: {
      ...corsHeaders,
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      'Connection': 'keep-alive',
    },
  });
}
