# Documentation Lessons

Evidence-backed reusable documentation lessons are appended here by `skill-runtime.py record`.


## L-25252537c13a

- Symptom: The copied documentation site lost its source top navigation and visual shell even though the article content was present.
- Root cause: The publication layer did not preserve the source Starlight component overrides and custom CSS as one coherent shell.
- Prevention: When reusing a documentation site, preserve Header, Sidebar, table-of-contents, Head, page-title, route middleware, and custom CSS together, then replace only navigation data and business content.
- Evidence: Real Chromium desktop and mobile screenshots show the restored shell; computed-layout checks confirm eight desktop navigation items and no horizontal overflow at 1440 and 390 pixels.


## L-6b9503208a2f

- Symptom: The copied documentation shell hid all top navigation on narrow screens and client-side navigation to a long article could widen the page.
- Root cause: The source theme hid desktop navigation below 50rem without a mobile replacement, and critical table and code overflow rules were page-scoped during ClientRouter transitions.
- Prevention: Keep desktop and mobile navigation backed by one data source, assert both in built output, and place responsive table and code overflow constraints in the global theme.
- Evidence: Docs gate passed; real Chrome measured 1440/1440 and 390/390 client-to-scroll widths, eight menu links, closed menu after navigation, complete title, and zero console errors.
