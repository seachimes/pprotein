# Scroll-boundary browser regression

Build the UI first, then run the standalone test from the repository root:

```sh
(cd view && pnpm run build)
npm install --prefix /tmp/pprotein-scroll-browser --no-save --no-package-lock playwright@1.60.0
PLAYWRIGHT_MODULE_ROOT=/tmp/pprotein-scroll-browser node view/tests/scroll-boundary.mjs
```

The test uses `/usr/bin/google-chrome` (override with `CHROME_BIN`) rather than downloading a browser. If Playwright is already resolvable beside the test, omit `PLAYWRIGHT_MODULE_ROOT`. `UI_DIST` can select a different built artifact directory. Dependencies are deliberately outside the app; no manifest/lockfile changes are necessary.

The test starts its own loopback HTTP server on an ephemeral port, serves `view/dist`, and supplies deterministic API responses. It never contacts an existing application process or real profiling data. It closes Chrome and its server in `finally`; when run from an agent, launch the command as a managed background job and collect its result.

For both `/diag/` and `/diff/`, at 1280×720 and 390×640, it:

- Waits for real rendered reports and fonts, and requires genuinely overflowing content.
- Uses Playwright's native mouse wheel input to prove the inner section scrolls.
- Reaches the bottom and top, continues 12 large wheel inputs past each boundary, and checks window/document/body/app/main positions plus header/nav geometry.
- Checks that the section stays within the viewport.
- Attempts programmatic root scrolling: `overflow: hidden` alone does not guarantee an overflowing root cannot be moved.
- Sends additional wheel input over the header and rejects application errors/unexpected API requests.

Optional negative control, expected to exit nonzero:

```sh
SCROLL_NEGATIVE_CONTROL=1 PLAYWRIGHT_MODULE_ROOT=/tmp/pprotein-scroll-browser node view/tests/scroll-boundary.mjs
```

This injects **browser-only** overrides matching the previous shared-section/root CSS (`position: static`, section overscroll containment, root default overscroll). It does not modify app sources or built files. It demonstrates that the test catches the previous escaped absolute-caption/root-overflow problem, but is not a substitute for separately checking out/building an earlier revision.

## Observed verification

On installed Google Chrome 144.0.7559.96, all four fixed-build cases passed. The negative control failed on desktop diag's programmatic root probe: document height 7210px versus viewport 720px, root scrollTop 6490px, and header top −6490px. Excess wheel input alone did **not** move the pre-fix root in this headless Linux run; the programmatic probe distinguishes the escaped-overflow defect. The negative control stops at its first failure, so it does not independently verify pre-fix diff behavior.

## Verification limits

This is a built-frontend layout/input regression with mocked API data, not a Go backend integration test. It tests headless installed Chrome on this platform; it does not establish Firefox/WebKit behavior, physical trackpad momentum, macOS/iOS rubber-banding, touch overscroll or browser pull-to-refresh. Native wheel events cannot fully simulate those OS gestures. The script does not automatically build: always rebuild after source changes to avoid testing stale assets. Responsive coverage is two fixed viewport sizes, not every device size.
