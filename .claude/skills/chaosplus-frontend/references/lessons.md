# Frontend Lessons

Evidence-backed reusable frontend lessons are appended here by `skill-runtime.py record`.

## L-59e185bcbd20

- Symptom: The standalone typecheck passed but the production build failed on Bun test types.
- Root cause: tsc --noEmit on the solution file did not traverse TypeScript project references, and the app project limited ambient types to vite/client.
- Prevention: Use tsc -b for the workspace application and include bun in the app project types whenever Bun tests are part of src.
- Evidence: bun run typecheck and bun run build both pass after the project-reference and ambient-type correction.


## L-b94712b1c400

- Symptom: A copied admin application sent browser API traffic to the source project localhost port instead of the Chaosplus /api proxy.
- Root cause: A copied .env.development retained the source project VITE_API_URL and overrode the repository proxy contract.
- Prevention: Audit copied environment files and require browser API traffic to use the single relative /api client path before accepting a copied frontend.
- Evidence: After removing the stale environment file, the real Chromium login and dashboard flow used /api and completed without 500 or 503 responses.


## L-a86db95e457e

- Symptom: A newly added protected administration route rendered correctly but deep-link login stayed on the login page with authentication unavailable.
- Root cause: The exact authn.web.allowed_return_urls deployment allowlist did not include the new route, so the backend rejected its return URL.
- Prevention: Whenever adding a protected frontend route, update every production and smoke allowed_return_urls list and verify anonymous deep-link login returns to that exact route with a real backend and browser.
- Evidence: The real Chromium OAuth Client workflow reached /iam/oauth-clients only after both Compose and smoke allowlists were updated; the full repository gate then passed at 90.0% Go race coverage.


## L-aaa73274d8b6

- Symptom: A repeat Passkey browser audit timed out waiting for the login form while an authenticated session remained in Chrome.
- Root cause: The audit assumed an anonymous browser and did not clear Cookie state before navigating to the login route.
- Prevention: Browser authentication audits must establish their initial Cookie state explicitly before asserting anonymous routes.
- Evidence: bun run audit:passkey 9333 http://localhost:8091 passed the registration, rename, passwordless login, deletion, rejection, desktop, and mobile checks after Network.clearBrowserCookies was added.


## L-93240317548d

- Symptom: Position member dates wrapped one character per line in the 390px dialog.
- Root cause: The dialog table had no stable minimum width, so all four columns were compressed into the mobile viewport.
- Prevention: Give dense multi-column dialog tables a stable minimum width inside their own overflow container and cap long dialogs to the viewport before mobile acceptance.
- Evidence: The repeated real Chromium position audit passed at 1440x1000 and 390x844; the mobile screenshot shows readable member and date columns with no page overflow.


## L-464c0be9a1c3

- Symptom: A shared directory-member dialog passed lint and typecheck but the real position audit could not find the stable member selector
- Root cause: The extracted component appended member to an input prefix that already contained member, producing position-member-member while sibling controls used the original prefix
- Prevention: Shared form components must preserve established DOM IDs and label associations; derive the primary control ID directly from the supplied prefix and verify existing real browser workflows after extraction
- Evidence: bun run audit:positions -- 9333 http://127.0.0.1:8091 passed after the fix, including desktop and mobile member dialogs


## L-23ff8353354c

- Symptom: Passkey browser audit timed out after registration and showed This is an invalid domain.
- Root cause: The audit URL used 127.0.0.1 while the configured WebAuthn RP ID was localhost, so the browser rejected credential creation before the API verify call.
- Prevention: Run WebAuthn browser acceptance on an origin whose effective domain exactly matches the configured RP ID; do not interchange localhost and 127.0.0.1.
- Evidence: bun run audit:passkey -- 9333 http://localhost:8091 passed registration, rename, passwordless login, deletion, rejection, and desktop/mobile layout checks.


## L-ea5eef09415c

- Symptom: The invitation browser audit reported a missing authorized navigation link even though the effective-menu API and final DOM contained it
- Root cause: The audit synchronously checked the sidebar after the page body loaded while effective menus were still loading asynchronously
- Prevention: Browser audits must wait with a bounded timeout for UI derived from asynchronous authorization data before asserting its presence
- Evidence: bun run audit:invitations -- 9333 http://127.0.0.1:8091 passed creation rotation revocation acceptance desktop mobile and zero-error checks
