// Standalone built-UI regression: see scroll-boundary.md for invocation and limits.
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { resolve, extname, sep } from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(
  process.env.PLAYWRIGHT_MODULE_ROOT
    ? resolve(process.env.PLAYWRIGHT_MODULE_ROOT, "package.json")
    : import.meta.url,
);
const { chromium } = require("playwright");
const dist = resolve(
  process.env.UI_DIST || fileURLToPath(new URL("../dist/", import.meta.url)),
);
await readFile(resolve(dist, "index.html")); // Fail early if the build is missing.
const groups = ["after", "before"].map((GroupID) => ({
  GroupID,
  Datetime: "2026-01-01T00:00:00Z",
  HasHTTPLog: true,
  HasSlowLog: true,
  HasPprof: false,
}));
const report = {
  GroupID: "after",
  Health: [],
  Summary: {
    TotalRequests: 1000,
    TotalAppTime: 100,
    TotalQueries: 2000,
    TotalQueryTime: 50,
    HasHTTPLog: true,
    HasSlowLog: true,
    HasPprof: false,
  },
  Findings: Array.from({ length: 32 }, (_, i) => ({
    Category: "INDEX",
    Title: `Finding ${i}`,
    Subject: `SELECT * FROM records WHERE id = ${i}`,
    ImpactShare: 0.2,
    Confidence: 0.9,
    Effort: 1,
    Score: 10,
    Evidence: [{ Label: "Query time", Value: "500ms" }],
    Suggestion: "Add an index and measure again.",
    Source: "slowlog",
  })),
};
const diff = {
  BeforeGroup: "before",
  AfterGroup: "after",
  Totals: {
    RequestsBefore: 1000,
    RequestsAfter: 1000,
    AppTimeBefore: 100,
    AppTimeAfter: 80,
    AppTimePct: -20,
    QueriesBefore: 2000,
    QueriesAfter: 2000,
    QueryTimeBefore: 50,
    QueryTimeAfter: 40,
    QueryTimePct: -20,
    ErrorsBefore: 0,
    ErrorsAfter: 0,
  },
  HTTP: Array.from({ length: 60 }, (_, i) => ({
    Endpoint: `GET /records/${i}`,
    CountBefore: 10,
    CountAfter: 10,
    CountDelta: 0,
    SumBefore: 1,
    SumAfter: 0.8,
    SumDelta: -0.2,
    AvgBefore: 0.1,
    AvgAfter: 0.08,
    AvgDelta: -0.02,
    AvgPct: -20,
    ErrBefore: 0,
    ErrAfter: 0,
    Status: "both",
    Direction: "improved",
  })),
  Query: Array.from({ length: 60 }, (_, i) => ({
    Query: `SELECT * FROM records WHERE id = ${i}`,
    CountBefore: 10,
    CountAfter: 10,
    CountDelta: 0,
    SumBefore: 1,
    SumAfter: 0.8,
    SumDelta: -0.2,
    SumPct: -20,
    ExaminedBefore: 100,
    ExaminedAfter: 10,
    Status: "both",
    Direction: "improved",
  })),
};
const unexpected = [];
const server = createServer(async (req, res) => {
  const url = new URL(req.url, "http://localhost");
  const json = (value) => {
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify(value));
  };
  if (url.pathname === "/api/diag/groups") return json(groups);
  if (url.pathname === "/api/diag/report/after") return json(report);
  if (
    url.pathname === "/api/diag/diff" &&
    url.searchParams.get("before") === "before" &&
    url.searchParams.get("after") === "after"
  )
    return json(diff);
  if (url.pathname === "/api/version")
    return json({
      Version: "scroll-fixture",
      Revision: "",
      Modified: false,
      GoVersion: "",
    });
  if (url.pathname === "/api/event") {
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    res.write(": fixture stream\n\n");
    return;
  }
  if (
    ["/api/memo", "/api/pprof", "/api/httplog", "/api/slowlog"].includes(
      url.pathname,
    )
  )
    return json([]);
  if (
    [
      "/api/group/targets",
      "/api/httplog/config",
      "/api/slowlog/config",
    ].includes(url.pathname)
  ) {
    res.end("");
    return;
  }
  if (url.pathname.startsWith("/api/")) {
    unexpected.push(url.pathname);
    res.writeHead(404);
    res.end("Unexpected API");
    return;
  }
  const path = resolve(dist, `.${decodeURIComponent(url.pathname)}`);
  if (!path.startsWith(dist + sep)) {
    res.writeHead(403);
    res.end();
    return;
  }
  try {
    const isRoute = ["/diag/", "/diff/"].includes(url.pathname);
    const file = isRoute ? resolve(dist, "index.html") : path;
    const content = await readFile(file);
    res.setHeader(
      "Content-Type",
      {
        ".html": "text/html",
        ".js": "text/javascript",
        ".css": "text/css",
        ".woff2": "font/woff2",
      }[extname(file)] || "application/octet-stream",
    );
    res.end(content);
  } catch {
    res.writeHead(404);
    res.end();
  }
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const baseURL = `http://127.0.0.1:${server.address().port}`;
let browser;
try {
  browser = await chromium.launch({
    executablePath: process.env.CHROME_BIN || "/usr/bin/google-chrome",
    headless: true,
  });
  console.log(
    `Testing ${dist} at isolated ${baseURL}; ${await browser.version()}`,
  );
  for (const viewport of [
    { width: 1280, height: 720 },
    { width: 390, height: 640 },
  ]) {
    const page = await browser.newPage({ viewport });
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("dialog", async (dialog) => {
      errors.push(dialog.message());
      await dialog.dismiss();
    });
    for (const route of ["diag", "diff"]) {
      await page.goto(`${baseURL}/index.html#/${route}/`);
      await page
        .locator(route === "diag" ? ".finding" : "table.diff tbody tr")
        .last()
        .waitFor();
      await page.evaluate(() => document.fonts.ready);
      // Optional negative control: reproduce pre-fix CSS in the browser only.
      if (process.env.SCROLL_NEGATIVE_CONTROL === "1") {
        await page.addStyleTag({
          content:
            "section { position: static !important; overscroll-behavior: contain !important; } html, body { overscroll-behavior: auto !important; }",
        });
      }
      const section = page.locator("main > section");
      const state = () =>
        page.evaluate(() => {
          const s = document.querySelector("main > section");
          const rect = (element) => {
            const r = element.getBoundingClientRect();
            return { top: r.top, bottom: r.bottom };
          };
          return {
            rootHeight: document.documentElement.scrollHeight,
            rootClientHeight: document.documentElement.clientHeight,
            x: scrollX,
            y: scrollY,
            htmlY: document.documentElement.scrollTop,
            bodyY: document.body.scrollTop,
            appY: document.querySelector("#app").scrollTop,
            mainY: document.querySelector("main").scrollTop,
            top: s.scrollTop,
            max: s.scrollHeight - s.clientHeight,
            header: rect(document.querySelector("header")),
            nav: rect(document.querySelector("nav")),
            section: rect(s),
            viewport: innerHeight,
          };
        });
      const initial = await state();
      assert.ok(
        initial.max > viewport.height,
        `${route}: fixture must genuinely overflow`,
      );
      const anchored = async (label) => {
        const s = await state();
        for (const key of ["x", "y", "htmlY", "bodyY", "appY", "mainY"])
          assert.equal(
            s[key],
            0,
            `${route} ${label}: ${key}; ${JSON.stringify(s)}`,
          );
        assert.deepEqual(
          s.header,
          initial.header,
          `${route} ${label}: header moved`,
        );
        assert.deepEqual(s.nav, initial.nav, `${route} ${label}: nav moved`);
        assert.ok(
          s.section.bottom <= s.viewport + 1,
          `${route}: section escapes viewport`,
        );
      };
      await anchored("initial");
      const box = await section.boundingBox();
      await page.mouse.move(
        Math.min(viewport.width - 20, box.x + box.width / 2),
        box.y + Math.min(box.height / 2, 180),
      );
      // Native input, not synthetic dispatchEvent. Poll because wheel() returns before scrolling settles.
      await page.mouse.wheel(0, 450);
      await page.waitForFunction(
        () => document.querySelector("main > section").scrollTop > 0,
      );
      await anchored("inner scroll");
      for (const direction of [1, -1]) {
        await page.mouse.wheel(0, direction * 100000);
        await page.waitForFunction((d) => {
          const s = document.querySelector("main > section");
          return d > 0
            ? Math.abs(s.scrollHeight - s.clientHeight - s.scrollTop) <= 1
            : s.scrollTop === 0;
        }, direction);
        // Continue wheel input well past both boundaries; frame waits allow compositor input processing.
        for (let i = 0; i < 12; i++) {
          await page.mouse.wheel(0, direction * 4000);
          await page.waitForTimeout(40);
          await anchored(direction > 0 ? "past bottom" : "past top");
        }
      }
      // overflow:hidden can still be programmatically scrolled if descendants escape its clip.
      await page.evaluate(() => {
        window.scrollTo(0, 100000);
        document.documentElement.scrollTop = 100000;
        document.body.scrollTop = 100000;
      });
      await anchored("programmatic root probe");
      await page.mouse.move(20, 15);
      await page.mouse.wheel(0, 100000);
      await page.waitForTimeout(150);
      await anchored("wheel over header");
      assert.deepEqual(errors, [], `${route}: browser errors`);
      assert.deepEqual(unexpected, [], "Unexpected API requests");
      console.log(
        `PASS ${route} ${viewport.width}x${viewport.height}: inner range=${initial.max}px; top/bottom excess wheel and root probe remained anchored`,
      );
    }
    await page.close();
  }
} finally {
  await browser?.close();
  server.closeAllConnections();
  await new Promise((resolve) => server.close(resolve));
}
