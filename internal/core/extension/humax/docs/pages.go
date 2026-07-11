package docs

// This file builds the static HTML for the API documentation UIs. Each renderer
// page is a tiny host page that loads its CDN bundle (latest, unversioned) and
// points it at the OpenAPI document. The spec URL is passed in by the caller
// (derived from huma's config.OpenAPIPath) rather than hardcoded, so changing
// the OpenAPI path keeps the docs working. The wrapper page embeds the renderers
// in an iframe with a tab switcher so all five are available at once. Pages are
// served by us (not huma), so they carry no restrictive CSP / frame-ancestors
// header and remain iframe-able by the wrapper.

// CDN assets for the docs UIs. These are unversioned URLs that resolve to each
// package's latest release, with no Subresource Integrity — so upstream updates
// are picked up automatically. That trades reproducibility/supply-chain pinning
// for convenience; acceptable for a dev docs surface, but pin versions and add
// integrity="..." here if these pages are ever exposed in production.
const (
	scalarJS = "https://unpkg.com/@scalar/api-reference/dist/browser/standalone.js"

	stoplightCSS = "https://unpkg.com/@stoplight/elements/styles.min.css"
	stoplightJS  = "https://unpkg.com/@stoplight/elements/web-components.min.js"

	swaggerCSS = "https://unpkg.com/swagger-ui-dist/swagger-ui.css"
	swaggerJS  = "https://unpkg.com/swagger-ui-dist/swagger-ui-bundle.js"

	redocJS     = "https://cdn.jsdelivr.net/npm/redoc/bundles/redoc.standalone.js"
	openapiUIJS = "https://cdn.jsdelivr.net/npm/openapi-ui-dist/lib/openapi-ui.umd.js"
)

// scalarHTML returns the Scalar API reference host page for the given spec URL.
func scalarHTML(name, spec string) []byte {
	return []byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>` + name + ` API — Scalar</title>
  </head>
  <body>
    <script id="api-reference" data-url="` + spec + `"></script>
    <script src="` + scalarJS + `" crossorigin></script>
  </body>
</html>`)
}

// swaggerHTML returns the Swagger UI host page for the given spec URL.
func swaggerHTML(name, spec string) []byte {
	return []byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>` + name + ` API — Swagger UI</title>
    <link rel="stylesheet" href="` + swaggerCSS + `" crossorigin>
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="` + swaggerJS + `" crossorigin></script>
    <script>
      window.onload = () => {
        window.ui = SwaggerUIBundle({ url: '` + spec + `', dom_id: '#swagger-ui' });
      };
    </script>
  </body>
</html>`)
}

// stoplightHTML returns the Stoplight Elements host page for the given spec URL
// (Stoplight is happy with either the JSON or YAML document).
func stoplightHTML(name, spec string) []byte {
	return []byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>` + name + ` API — Stoplight</title>
    <link rel="stylesheet" href="` + stoplightCSS + `" crossorigin>
    <script src="` + stoplightJS + `" crossorigin></script>
  </head>
  <body style="height: 100vh;">
    <elements-api
      apiDescriptionUrl="` + spec + `"
      router="hash"
      layout="sidebar"
      tryItCredentialsPolicy="same-origin"
    ></elements-api>
  </body>
</html>`)
}

// redocHTML returns the ReDoc host page for the given spec URL.
func redocHTML(name, spec string) []byte {
	return []byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>` + name + ` API — ReDoc</title>
  </head>
  <body>
    <div id="redoc"></div>
    <script src="` + redocJS + `" crossorigin></script>
    <script>Redoc.init('` + spec + `', {}, document.getElementById('redoc'));</script>
  </body>
</html>`)
}

// openapiUIHTML returns the openapi-ui host page for the given spec URL.
func openapiUIHTML(name, spec string) []byte {
	return []byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>` + name + ` API — openapi-ui</title>
  </head>
  <body>
    <div id="openapi-ui-container" spec-url="` + spec + `" theme="light"></div>
    <script src="` + openapiUIJS + `" crossorigin></script>
  </body>
</html>`)
}

// docsWrapperHTML returns the tabbed shell that embeds each renderer in an
// iframe. Tabs are keyboard-accessible and the active renderer is reflected in
// the URL hash (e.g. /docs#swagger) for deep-linking. specJSON is linked as the
// raw-spec shortcut in the top bar.
func docsWrapperHTML(name, specJSON string) []byte {
	return []byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>` + name + ` API — Docs</title>
    <style>
      :root {
        --bar-h: 48px;
        --bg: #0f1115;
        --bar: #171a21;
        --border: #262b36;
        --text: #e6e9ef;
        --muted: #9aa4b2;
        --accent: #6ea8fe;
        --radius: 8px;
        --dur: 160ms;
      }
      * { box-sizing: border-box; }
      html, body { margin: 0; height: 100%; }
      body {
        font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
        background: var(--bg); color: var(--text);
      }
      .bar {
        height: var(--bar-h); display: flex; align-items: center; gap: .25rem;
        padding: 0 .75rem; background: var(--bar); border-bottom: 1px solid var(--border);
      }
      .brand { font-weight: 600; font-size: .9rem; letter-spacing: .02em; margin-right: .75rem; white-space: nowrap; }
      .brand span { color: var(--muted); font-weight: 400; }
      .tabs { display: flex; gap: .25rem; }
      .tab {
        appearance: none; border: 1px solid transparent; background: transparent; color: var(--muted);
        font: inherit; font-size: .85rem; padding: .35rem .7rem; border-radius: var(--radius); cursor: pointer;
        transition: color var(--dur), background var(--dur), border-color var(--dur);
      }
      .tab:hover { color: var(--text); background: rgba(255,255,255,.04); }
      .tab:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
      .tab[aria-selected="true"] {
        color: var(--text); background: rgba(110,168,254,.14); border-color: rgba(110,168,254,.35);
      }
      .spacer { flex: 1; }
      .ext {
        color: var(--muted); font-size: .8rem; text-decoration: none; padding: .35rem .6rem;
        border-radius: var(--radius); transition: color var(--dur), background var(--dur);
      }
      .ext:hover { color: var(--text); background: rgba(255,255,255,.04); }
      .frame-wrap { height: calc(100% - var(--bar-h)); background: #fff; }
      iframe { width: 100%; height: 100%; border: 0; display: block; }
      @media (prefers-reduced-motion: reduce) { * { transition: none !important; } }
    </style>
  </head>
  <body>
    <header class="bar">
      <div class="brand">` + name + ` API <span>docs</span></div>
      <nav class="tabs" role="tablist" aria-label="Documentation renderer">
        <button class="tab" role="tab" data-src="/docs/scalar" aria-selected="true">scalar</button>
        <button class="tab" role="tab" data-src="/docs/stoplight" aria-selected="false">stoplight</button>
        <button class="tab" role="tab" data-src="/docs/redoc" aria-selected="false">redoc</button>
        <button class="tab" role="tab" data-src="/docs/openapi-ui" aria-selected="false">openapi-ui</button>
        <button class="tab" role="tab" data-src="/docs/swagger" aria-selected="false">swagger-ui</button>
      </nav>
      <div class="spacer"></div>
      <a class="ext" href="` + specJSON + `" target="_blank" rel="noreferrer">` + specJSON + ` &#8599;</a>
    </header>
    <div class="frame-wrap">
      <iframe id="frame" title="API documentation" src="/docs/scalar"></iframe>
    </div>
    <script>
      (function () {
        var frame = document.getElementById('frame');
        var tabs = Array.prototype.slice.call(document.querySelectorAll('.tab'));
        function key(src) { return src.split('/').pop(); }
        function select(tab) {
          tabs.forEach(function (t) { t.setAttribute('aria-selected', String(t === tab)); });
          var src = tab.getAttribute('data-src');
          if (frame.getAttribute('src') !== src) { frame.setAttribute('src', src); }
          try { history.replaceState(null, '', '#' + key(src)); } catch (e) {}
        }
        tabs.forEach(function (t) { t.addEventListener('click', function () { select(t); }); });
        var hash = (location.hash || '').replace('#', '');
        var match = tabs.filter(function (t) { return key(t.getAttribute('data-src')) === hash; })[0];
        if (match) { select(match); }
      })();
    </script>
  </body>
</html>`)
}
