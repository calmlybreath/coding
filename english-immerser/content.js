// ============================================================
//  English Immerser — Content Script
//  Extracts page text, applies LLM replacements, renders corner card
// ============================================================

const EI_PREFIX = 'ei-';
const MAX_TEXT_LENGTH = 4000; // max chars to send to LLM

// Current page state
let allReplacements = [];   // [{zh, en, id}] applied on this page

// ============================================================
//  Init
// ============================================================

async function init() {
  // Don't run in iframes
  if (window !== window.top) return;

  const settings = await chrome.storage.local.get({
    enabled: true,
    density: 'medium'
  });

  console.log('[EnglishImmerser] Settings:', settings);
  if (!settings.enabled) {
    console.log('[EnglishImmerser] Disabled, skipping');
    return;
  }

  // Create tooltip element for hover display (always needed)
  createTooltipElement();
  setupTooltipListeners();

  // Try to process content that's already on the page
  await tryProcessContent(settings.density);

  // Watch for dynamic content — this ALWAYS runs, even if page is empty now
  // (critical for AJAX-rendered pages like 雪球/微博/知乎)
  setupMutationObserver();
}

async function tryProcessContent(density) {
  const { title, elements } = extractPageContent();
  console.log('[EnglishImmerser] Extracted', elements.length, 'elements, title:', title);
  if (elements.length === 0) return;

  const fullText = buildFullText(title, elements);
  console.log('[EnglishImmerser] Full text length:', fullText.length, 'chars');

  const replacements = await requestReplacements(title, fullText, density);
  console.log('[EnglishImmerser] Got', replacements?.length || 0, 'replacements from background');
  if (!replacements || replacements.length === 0) return;

  const applied = applyReplacements(replacements, elements);
  console.log('[EnglishImmerser] Applied', applied.length, 'replacements to DOM');

  allReplacements = applied;
}

// ============================================================
//  Extract page content — tag-agnostic, finds leaf content blocks
// ============================================================

const SKIP_SELECTOR = [
  'script', 'style', 'noscript', 'textarea', 'input', 'select',
  'nav', 'footer', 'header', 'aside',
  '.sidebar', '.nav', '.menu', '.footer', '.header',
  '.ei-tooltip', '[class*="ei-"]',
  'code', 'pre', 'kbd', 'samp', 'svg', 'img', 'video', 'audio',
  'iframe', 'canvas', 'template'
].join(',');

// Content containers — try these first to scope the search
const CONTAINER_SELECTORS = [
  'article', '[role="main"]', 'main',
  '.post-content', '.article-content', '.entry-content',
  '.markdown-body', '.content', '#content',
  '.post', '.article', '.blog-post'
];

function extractPageContent() {
  const title = document.title || '';

  // Find a content container, fallback to body
  let container = null;
  for (const sel of CONTAINER_SELECTORS) {
    container = document.querySelector(sel);
    if (container && container.textContent.trim().length > 100) break;
  }
  if (!container) container = document.body;

  const elements = findContentBlocks(container);
  console.log('[EnglishImmerser] Found', elements.length, 'content blocks in container:', container.tagName, container.className?.slice(0, 40) || '');

  // Limit total text
  let totalChars = 0;
  const limited = [];
  for (const el of elements) {
    totalChars += el.textContent.length;
    if (totalChars > MAX_TEXT_LENGTH * 2) break;
    limited.push(el);
  }

  return { title, elements: limited };
}

// Find all "leaf" content blocks — elements with Chinese text
// that don't have child elements also qualifying as content blocks
function findContentBlocks(container) {
  const allBlocks = [];

  // First pass: collect all candidate elements
  const walker = document.createTreeWalker(
    container,
    NodeFilter.SHOW_ELEMENT,
    {
      acceptNode: (node) => {
        if (node.matches && node.matches(SKIP_SELECTOR)) return NodeFilter.FILTER_REJECT;
        const text = node.textContent.trim();
        if (text.length < 15) return NodeFilter.FILTER_SKIP;
        if (!/[一-鿿]/.test(text)) return NodeFilter.FILTER_SKIP;
        return NodeFilter.FILTER_ACCEPT;
      }
    }
  );

  while (walker.nextNode()) {
    allBlocks.push(walker.currentNode);
  }

  // Second pass: keep only leaves — remove elements that contain other candidates
  const blockSet = new Set(allBlocks);
  const leaves = allBlocks.filter(el => {
    let parent = el.parentElement;
    while (parent && parent !== container) {
      if (blockSet.has(parent)) return false; // ancestor is also a content block → skip
      parent = parent.parentElement;
    }
    return true;
  });

  return leaves;
}

function buildFullText(title, elements) {
  const parts = [`标题: ${title}\n`];
  for (let i = 0; i < elements.length; i++) {
    const text = elements[i].textContent.trim();
    parts.push(`[P${i}] ${text}`);
  }
  return parts.join('\n\n');
}

// ============================================================
//  Request replacements from background (LLM)
// ============================================================

function requestReplacements(title, fullText, density) {
  return new Promise((resolve) => {
    chrome.runtime.sendMessage(
      {
        type: 'GET_REPLACEMENTS',
        url: window.location.href,
        title,
        text: fullText.slice(0, MAX_TEXT_LENGTH),
        density
      },
      (response) => {
        if (chrome.runtime.lastError) {
          console.warn('[EnglishImmerser] Message error:', chrome.runtime.lastError);
          resolve([]);
          return;
        }
        resolve(response?.replacements || []);
      }
    );
  });
}

// ============================================================
//  Apply replacements to DOM
// ============================================================

function applyReplacements(replacements, elements) {
  const applied = [];

  for (let idx = 0; idx < replacements.length; idx++) {
    const { zh, en, p } = replacements[idx];
    if (!zh || !en) continue;
    if (zh.length < 2) continue;

    // If LLM told us which paragraph this belongs to, target it directly
    if (p !== undefined && elements[p]) {
      if (tryReplaceInElement(elements[p], zh, en, idx)) {
        applied.push({ zh, en, id: `ei-r-${idx}` });
        continue;
      }
    }

    // Fallback: search all elements (for cached results without p, or if target element missed)
    for (const el of elements) {
      if (tryReplaceInElement(el, zh, en, idx)) {
        applied.push({ zh, en, id: `ei-r-${idx}` });
        break;
      }
    }
  }

  return applied;
}

function tryReplaceInElement(el, searchText, replacementText, idx) {
  // Per-element tracking: same zh won't be replaced twice in the same element
  // This allows SAME zh with DIFFERENT en in different contexts
  if (!el.dataset.eiPhrases) el.dataset.eiPhrases = '';
  if (el.dataset.eiPhrases.includes(`|${searchText}|`)) return false;

  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, {
    acceptNode: (node) => {
      const parent = node.parentElement;
      if (!parent) return NodeFilter.FILTER_REJECT;
      if (parent.closest?.('.ei-card, .ei-card-mini-dot, .ei-tooltip')) return NodeFilter.FILTER_REJECT;
      if (parent.tagName === 'SCRIPT' || parent.tagName === 'STYLE' ||
          parent.tagName === 'NOSCRIPT' || parent.tagName === 'TEXTAREA' ||
          parent.tagName === 'INPUT') return NodeFilter.FILTER_REJECT;
      if (!node.textContent.includes(searchText)) return NodeFilter.FILTER_REJECT;
      return NodeFilter.FILTER_ACCEPT;
    }
  });

  const node = walker.nextNode();
  if (!node) return false;

  // Do the replacement
  const text = node.textContent;
  const startIdx = text.indexOf(searchText);
  if (startIdx === -1) return false;

  const endIdx = startIdx + searchText.length;
  const parent = node.parentNode;
  const frag = document.createDocumentFragment();

  // Text before match
  if (startIdx > 0) {
    frag.appendChild(document.createTextNode(text.slice(0, startIdx)));
  }

  // Replacement span
  const span = document.createElement('span');
  span.className = 'ei-replaced';
  span.textContent = replacementText;
  span.dataset.zh = searchText;
  span.dataset.eiId = `ei-r-${idx}`;
  frag.appendChild(span);

  // Text after match
  if (endIdx < text.length) {
    frag.appendChild(document.createTextNode(text.slice(endIdx)));
  }

  parent.replaceChild(frag, node);

  // Mark this specific phrase as replaced in this element
  // Allows: ① multiple different phrases in one paragraph
  //         ② same zh with different en in different contexts (LLM returns both)
  el.dataset.eiPhrases += `|${searchText}|`;
  return true;
}

function createTooltipElement() {
  const tooltip = document.createElement('div');
  tooltip.className = 'ei-tooltip';
  tooltip.id = 'ei-tooltip';
  document.body.appendChild(tooltip);
}

// ============================================================
//  Tooltip — show Chinese on hover
// ============================================================

function setupTooltipListeners() {
  document.addEventListener('mouseover', (e) => {
    const replaced = e.target.closest?.('.ei-replaced');
    if (!replaced) return;
    const zh = replaced.dataset.zh;
    if (!zh) return;
    showTooltip(e, zh);
  }, true);

  document.addEventListener('mouseout', (e) => {
    const replaced = e.target.closest?.('.ei-replaced');
    if (!replaced) return;
    hideTooltip();
  }, true);

  document.addEventListener('mousemove', (e) => {
    const replaced = e.target.closest?.('.ei-replaced');
    if (!replaced) return;
    moveTooltip(e);
  }, true);
}

function showTooltip(event, text) {
  const tooltip = document.getElementById('ei-tooltip');
  if (!tooltip) return;
  tooltip.textContent = text;
  tooltip.className = 'ei-tooltip ei-visible';
  moveTooltip(event);
}

function moveTooltip(event) {
  const tooltip = document.getElementById('ei-tooltip');
  if (!tooltip) return;
  // Position above and to the right of cursor
  const gap = 14;
  const x = event.clientX + gap;
  const y = event.clientY - 34;
  // Keep tooltip within viewport
  const rect = tooltip.getBoundingClientRect();
  const adjustedX = Math.min(x, window.innerWidth - rect.width - 8);
  const adjustedY = Math.max(8, y);
  tooltip.style.left = adjustedX + 'px';
  tooltip.style.top = adjustedY + 'px';
}

function hideTooltip() {
  const tooltip = document.getElementById('ei-tooltip');
  if (!tooltip) return;
  tooltip.className = 'ei-tooltip';
}

// ============================================================
//  MutationObserver — handle dynamically loaded content
// ============================================================

function setupMutationObserver() {
  let debounceTimer = null;

  const observer = new MutationObserver((mutations) => {
    // Debounce: wait for mutations to settle
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      processNewContent(mutations);
    }, 2000);
  });

  observer.observe(document.body, {
    childList: true,
    subtree: true
  });
}

async function processNewContent(mutations) {
  const settings = await chrome.storage.local.get({ enabled: true, density: 'medium' });
  if (!settings.enabled) return;

  // Collect newly added elements that could contain content
  const newRoots = [];
  for (const mutation of mutations) {
    for (const node of mutation.addedNodes) {
      if (node.nodeType !== Node.ELEMENT_NODE) continue;
      if (node.closest?.('.ei-tooltip')) continue;
      newRoots.push(node);
    }
  }

  if (newRoots.length === 0) return;

  // Use the same tag-agnostic content block finder
  const allNewElements = [];
  for (const root of newRoots) {
    const blocks = findContentBlocks(root);
    for (const b of blocks) {
      if (!b.dataset.eiPhrases) {
        allNewElements.push(b);
      }
    }
  }

  console.log('[EnglishImmerser] MutationObserver found', allNewElements.length, 'new content blocks');
  if (allNewElements.length === 0) return;

  const fullText = buildFullText('', allNewElements);

  const replacements = await requestReplacements(document.title, fullText, settings.density);
  if (!replacements || replacements.length === 0) return;

  const applied = applyReplacements(replacements, allNewElements);
  console.log('[EnglishImmerser] MutationObserver applied', applied.length, 'replacements');
  if (applied.length === 0) return;

  allReplacements = [...allReplacements, ...applied];
}

// ============================================================
//  Listen for settings changes
// ============================================================

chrome.storage.onChanged.addListener((changes, areaName) => {
  if (areaName !== 'local') return;
  if (changes.enabled) {
    if (!changes.enabled.newValue) {
      // Disabled: remove tooltip element
      const tooltip = document.getElementById('ei-tooltip');
      if (tooltip) tooltip.remove();
      // Note: can't easily undo text replacements, page refresh needed
    }
  }
});

// ============================================================
//  Start
// ============================================================

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}
