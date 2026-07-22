// ============================================================
//  English Immerser — Background Service Worker
//  Handles LLM API calls + in-memory caching
// ============================================================

const DEFAULT_ENDPOINT = 'https://api.deepseek.com/chat/completions';
const DEFAULT_MODEL = 'deepseek-v4-flash';
const CACHE_TTL_MS = 24 * 60 * 60 * 1000; // 24 hours

// In-memory cache: url → { replacements, timestamp }
const cache = new Map();

// ============================================================
//  Message handler
// ============================================================

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === 'GET_REPLACEMENTS') {
    handleGetReplacements(message, sender, sendResponse);
    return true; // async response
  }
});

// ============================================================
//  Core: get replacements (cached or via LLM)
// ============================================================

async function handleGetReplacements(message, sender, sendResponse) {
  const { url, title, text, density } = message;

  // Cache by content hash — same content = same replacements
  // This correctly handles AJAX pages where URL stays but content changes
  const cacheKey = hash(text.slice(0, 500) + title);

  // Check cache
  const cached = cache.get(cacheKey);
  if (cached && Date.now() - cached.timestamp < CACHE_TTL_MS) {
    console.log('[EnglishImmerser] Cache hit for', url);
    sendResponse({ replacements: cached.replacements, fromCache: true });
    return;
  }

  // Read settings
  const settings = await chrome.storage.local.get(['apiKey', 'endpoint', 'model']);

  const apiKey = settings.apiKey;
  if (!apiKey) {
    console.log('[EnglishImmerser] No API key configured, skipping');
    sendResponse({ replacements: [], error: 'No API key' });
    return;
  }

  const endpoint = settings.endpoint || DEFAULT_ENDPOINT;
  const model = settings.model || DEFAULT_MODEL;

  // Build prompt
  const countRange = densityToCount(density);
  const prompt = buildPrompt(title, text, countRange);

  // Call LLM
  try {
    const replacements = await callLLM(endpoint, apiKey, model, prompt);
    // Only cache if we got actual results (don't cache empty — it poisons the cache)
    if (replacements.length > 0) {
      cache.set(cacheKey, { replacements, timestamp: Date.now() });
      cleanCache();
    }
    console.log('[EnglishImmerser] LLM returned', replacements.length, 'replacements, cached:', replacements.length > 0);
    sendResponse({ replacements, fromCache: false });
  } catch (err) {
    console.error('[EnglishImmerser] LLM call failed:', err.message);
    sendResponse({ replacements: [], error: err.message });
  }
}

// ============================================================
//  LLM API call
// ============================================================

async function callLLM(endpoint, apiKey, model, prompt) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 15000);

  console.log('[EnglishImmerser] === LLM REQUEST ===');
  console.log('[EnglishImmerser] Endpoint:', endpoint);
  console.log('[EnglishImmerser] Model:', model);
  console.log('[EnglishImmerser] Prompt length:', prompt.length, 'chars');
  console.log('[EnglishImmerser] Prompt:\n', prompt);
  console.log('[EnglishImmerser] === END REQUEST ===');

  try {
    const res = await fetch(endpoint, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${apiKey}`
      },
      body: JSON.stringify({
        model: model,
        messages: [
          { role: 'system', content: SYSTEM_PROMPT },
          { role: 'user', content: prompt }
        ],
        temperature: 0.3,
        max_tokens: 2000
      }),
      signal: controller.signal
    });

    if (!res.ok) {
      const body = await res.text();
      throw new Error(`HTTP ${res.status}: ${body.slice(0, 200)}`);
    }

    const data = await res.json();
    const content = data.choices?.[0]?.message?.content || '';

    console.log('[EnglishImmerser] === LLM RESPONSE ===');
    console.log('[EnglishImmerser] Raw response:\n', content);
    console.log('[EnglishImmerser] === END RESPONSE ===');

    // Extract JSON array from response
    const parsed = parseResponse(content);
    console.log('[EnglishImmerser] Parsed replacements:', JSON.stringify(parsed, null, 2));
    return parsed;
  } finally {
    clearTimeout(timeout);
  }
}

// ============================================================
//  Prompt engineering
// ============================================================

const SYSTEM_PROMPT = `You are an assistant helping a Chinese native speaker learn English through immersion.
Given Chinese text from a webpage, pick suitable Chinese phrases and provide natural English translations.

Rules:
- Pick 2-6 character Chinese phrases (not single characters, not full sentences)
- Choose phrases with practical value (daily, business, tech expressions)
- The English translation must be natural and idiomatic, considering the phrase's context in its paragraph
- Skip proper nouns, personal names, place names
- Each paragraph is marked with [P0], [P1], etc. — include the paragraph index as "p" in your response
- The SAME Chinese phrase in DIFFERENT paragraphs may need DIFFERENT translations
- Return ONLY a JSON array, no other text`;

function buildPrompt(title, text, countRange) {
  return `Pick ${countRange} Chinese phrases from this webpage and translate them to natural English.
Each paragraph is marked with [P0], [P1] etc. — include the "p" field to indicate which paragraph the phrase comes from.

Page title: ${title}

Page content:
${text}

Return ONLY a JSON array like:
[{"zh":"系统架构","en":"system architecture","p":0}, {"zh":"操作","en":"operate","p":3}, {"zh":"操作","en":"procedure","p":7}]`;
}

function densityToCount(density) {
  switch (density) {
    case 'low':    return '3-5';
    case 'high':   return '10-20';
    case 'medium':
    default:       return '5-10';
  }
}

// ============================================================
//  Response parser — robustly extract JSON from LLM output
// ============================================================

function parseResponse(content) {
  // Try direct JSON parse first
  try {
    const arr = JSON.parse(content.trim());
    if (Array.isArray(arr) && arr.every(item => item.zh && item.en)) {
      return arr.map(({ zh, en, p }) => ({ zh, en, p }));
    }
  } catch (_) { /* fall through */ }

  // Try to extract JSON array from markdown code blocks or surrounding text
  const match = content.match(/\[[\s\S]*\]/);
  if (match) {
    try {
      const arr = JSON.parse(match[0]);
      if (Array.isArray(arr) && arr.every(item => item.zh && item.en)) {
        return arr.map(({ zh, en, p }) => ({ zh, en, p }));
      }
    } catch (_) { /* fall through */ }
  }

  // Try line-by-line parsing as fallback
  const lines = content.split('\n').filter(l => l.includes('"zh"') || l.includes('"en"'));
  if (lines.length > 0) {
    try {
      const arr = JSON.parse('[' + lines.join(',') + ']');
      if (Array.isArray(arr)) return arr.map(({ zh, en, p }) => ({ zh, en, p }));
    } catch (_) { /* fall through */ }
  }

  console.warn('[EnglishImmerser] Could not parse LLM response:', content.slice(0, 300));
  return [];
}

// ============================================================
//  Utilities
// ============================================================

function hash(str) {
  let h = 0;
  for (let i = 0; i < str.length; i++) {
    h = ((h << 5) - h + str.charCodeAt(i)) | 0;
  }
  return h.toString(36);
}

function cleanCache() {
  const now = Date.now();
  for (const [key, val] of cache) {
    if (now - val.timestamp > CACHE_TTL_MS) {
      cache.delete(key);
    }
  }
}
