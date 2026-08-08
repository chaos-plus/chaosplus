const port = Number(process.argv[2] ?? 9333)
const baseURL = process.argv[3] ?? "http://localhost:8091"
const password = process.env.CHAOSPLUS_BROWSER_PASSWORD
const screenshotRoot = new URL(
  "../../../../../.local/screenshots/",
  import.meta.url
)

if (!password) throw new Error("CHAOSPLUS_BROWSER_PASSWORD is required")

const targets = await fetch(`http://127.0.0.1:${port}/json/list`).then(
  (response) => response.json()
)
const target = targets.find((candidate) => candidate.type === "page")
if (!target?.webSocketDebuggerUrl)
  throw new Error("Chrome page target was not found")

const socket = new WebSocket(target.webSocketDebuggerUrl)
await new Promise((resolve, reject) => {
  socket.addEventListener("open", resolve, { once: true })
  socket.addEventListener("error", reject, { once: true })
})

let sequence = 0
const pending = new Map()
const requestByID = new Map()
const responses = []
const failedRequests = []
const consoleErrors = []
socket.addEventListener("message", (event) => {
  const message = JSON.parse(event.data)
  if (message.id) {
    const request = pending.get(message.id)
    if (!request) return
    pending.delete(message.id)
    if (message.error) request.reject(new Error(message.error.message))
    else request.resolve(message.result)
    return
  }
  if (message.method === "Network.requestWillBeSent")
    requestByID.set(message.params.requestId, {
      method: message.params.request.method,
      url: message.params.request.url,
    })
  if (message.method === "Network.responseReceived") {
    const request = requestByID.get(message.params.requestId)
    responses.push({
      method: request?.method ?? "GET",
      url: message.params.response.url,
      status: message.params.response.status,
    })
  }
  if (message.method === "Network.loadingFailed") {
    const request = requestByID.get(message.params.requestId)
    failedRequests.push({
      method: request?.method ?? "GET",
      url: request?.url ?? "unknown",
      error: message.params.errorText,
      canceled: Boolean(message.params.canceled),
    })
  }
  if (message.method === "Runtime.exceptionThrown")
    consoleErrors.push(message.params.exceptionDetails.text)
  if (
    message.method === "Runtime.consoleAPICalled" &&
    message.params.type === "error"
  )
    consoleErrors.push(
      message.params.args
        .map((argument) => argument.value ?? argument.description)
        .join(" ")
    )
})

function command(method, params = {}) {
  const id = ++sequence
  socket.send(JSON.stringify({ id, method, params }))
  return new Promise((resolve, reject) => pending.set(id, { resolve, reject }))
}

async function evaluate(source) {
  const result = await command("Runtime.evaluate", {
    expression: `(async () => { ${source} })()`,
    awaitPromise: true,
    returnByValue: true,
  })
  if (result.exceptionDetails)
    throw new Error(
      result.exceptionDetails.exception?.description ??
        result.exceptionDetails.text
    )
  return result.result.value
}

async function waitFor(source, timeout = 15000) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) {
    if (await evaluate(`return Boolean(${source})`)) return
    await Bun.sleep(100)
  }
  throw new Error(`Timed out waiting for: ${source}`)
}

async function navigate(path) {
  await command("Page.navigate", { url: `${baseURL}${path}` })
  await waitFor(`document.readyState === "complete"`)
}

async function setInput(selector, value) {
  await evaluate(`
    const input = document.querySelector(${JSON.stringify(selector)});
    if (!(input instanceof HTMLInputElement)) throw new Error("input not found");
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
    setter.call(input, ${JSON.stringify(value)});
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  `)
}

async function submitDepartmentForm() {
  await evaluate(`
    const button = document.querySelector('button[form="department-form"][type="submit"]');
    if (!(button instanceof HTMLButtonElement)) throw new Error("submit button not found");
    button.click();
  `)
  try {
    await waitFor(`!document.querySelector("#department-form")`)
  } catch (cause) {
    const diagnostic = await evaluate(`
      const form = document.querySelector("#department-form");
      return {
        body: document.body.innerText.slice(0, 4000),
        formValid: form instanceof HTMLFormElement ? form.checkValidity() : null,
        invalid: form instanceof HTMLFormElement
          ? [...form.querySelectorAll(":invalid")].map((item) => ({ id: item.id, validationMessage: item.validationMessage }))
          : [],
        alert: document.querySelector('[role="alert"]')?.textContent ?? "",
      };
    `)
    const image = await command("Page.captureScreenshot", {
      format: "png",
      fromSurface: true,
      captureBeyondViewport: false,
    })
    await Bun.write(
      new URL("admin-departments-diagnostic.png", screenshotRoot),
      Buffer.from(image.data, "base64")
    )
    const departmentResponses = responses
      .filter((response) => response.url.includes("/api/iam/departments"))
      .slice(-10)
    throw new Error(
      `${cause.message}; diagnostic=${JSON.stringify(diagnostic)}; responses=${JSON.stringify(departmentResponses)}`
    )
  }
}

async function selectOption(triggerSelector, label) {
  await evaluate(`
    const trigger = document.querySelector(${JSON.stringify(triggerSelector)});
    if (!(trigger instanceof HTMLButtonElement)) throw new Error("select trigger not found");
    trigger.click();
  `)
  await waitFor(`document.querySelector('[role="option"]')`)
  const point = await evaluate(`
    const option = [...document.querySelectorAll('[role="option"]')].find(
      (item) => item.textContent.includes(${JSON.stringify(label)})
    );
    if (!(option instanceof HTMLElement)) throw new Error("select option not found");
    const rect = option.getBoundingClientRect();
    return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
  `)
  await command("Input.dispatchMouseEvent", {
    type: "mousePressed",
    x: point.x,
    y: point.y,
    button: "left",
    clickCount: 1,
  })
  await command("Input.dispatchMouseEvent", {
    type: "mouseReleased",
    x: point.x,
    y: point.y,
    button: "left",
    clickCount: 1,
  })
  await waitFor(
    `document.querySelector(${JSON.stringify(triggerSelector)})?.textContent.includes(${JSON.stringify(label)})`
  )
  await waitFor(
    `document.querySelector(${JSON.stringify(triggerSelector)})?.getAttribute("aria-expanded") !== "true"`
  )
}

function rowExpression(name) {
  return `[...document.querySelectorAll("tbody tr")].find((row) => [...row.querySelectorAll("strong")].some((item) => item.textContent.trim() === ${JSON.stringify(name)}))`
}

async function openCreateDialog() {
  await evaluate(`
    const button = document.querySelector("header:has(h1) button");
    if (!(button instanceof HTMLButtonElement)) throw new Error("create button not found");
    button.click();
  `)
  await waitFor(`document.querySelector("#department-form")`)
}

async function createDepartment(name, sortOrder, parentName = "") {
  await openCreateDialog()
  await setInput("#department-name", name)
  await setInput("#department-sort-order", String(sortOrder))
  if (parentName) await selectOption("#department-parent", parentName)
  await submitDepartmentForm()
  await waitFor(rowExpression(name))
}

async function clickRowAction(name, fromEnd) {
  await evaluate(`
    const row = ${rowExpression(name)};
    if (!(row instanceof HTMLTableRowElement)) throw new Error("department row not found");
    const buttons = [...row.querySelectorAll("button[title]")];
    const button = buttons.at(-${fromEnd});
    if (!(button instanceof HTMLButtonElement)) throw new Error("row action not found");
    button.click();
  `)
}

async function departments() {
  return evaluate(`
    const response = await fetch("/api/iam/departments", {
      credentials: "include",
      headers: { "X-Tenant-Id": localStorage.getItem("chaosplus.tenant") || "platform" },
    });
    if (!response.ok) throw new Error("department list failed: " + response.status);
    return (await response.json()).data;
  `)
}

async function cleanupArtifacts() {
  await evaluate(`
    const tenant = localStorage.getItem("chaosplus.tenant") || "platform";
    const headers = { "X-Tenant-Id": tenant };
    const response = await fetch("/api/iam/departments", { credentials: "include", headers });
    if (!response.ok) throw new Error("artifact list failed: " + response.status);
    const items = (await response.json()).data;
    const pattern = /^(Acceptance-(Engineering|Operations)|Platform-(Team|Engineering))-[a-z0-9]+$/;
    const artifacts = items.filter((item) => pattern.test(item.name)).sort((left, right) => right.depth - left.depth);
    for (const item of artifacts) {
      const removed = await fetch(
        "/api/iam/departments/" + encodeURIComponent(item.id) + "?version=" + encodeURIComponent(item.version),
        { method: "DELETE", credentials: "include", headers }
      );
      if (!removed.ok && removed.status !== 404)
        throw new Error("artifact cleanup failed: " + removed.status);
    }
  `)
  await command("Page.reload", { ignoreCache: true })
  await waitFor(`document.readyState === "complete"`)
  await waitFor(`document.querySelector("h1") && document.querySelector("tbody")`)
}

async function screenshot(name, width, height) {
  await command("Emulation.setDeviceMetricsOverride", {
    width,
    height,
    deviceScaleFactor: 1,
    mobile: width < 600,
  })
  await Bun.sleep(250)
  const layout = await evaluate(`
    return {
      width: innerWidth,
      scrollWidth: document.documentElement.scrollWidth,
      horizontalOverflow: document.documentElement.scrollWidth > innerWidth,
    };
  `)
  if (layout.horizontalOverflow)
    throw new Error(`Horizontal overflow at ${width}px: ${layout.scrollWidth}px`)
  const image = await command("Page.captureScreenshot", {
    format: "png",
    fromSurface: true,
    captureBeyondViewport: false,
  })
  await Bun.write(
    new URL(name, screenshotRoot),
    Buffer.from(image.data, "base64")
  )
  return layout
}

await Promise.all([
  command("Network.enable"),
  command("Page.enable"),
  command("Runtime.enable"),
])
await command("Network.clearBrowserCookies")
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})

const suffix = Date.now().toString(36)
const rootA = `Acceptance-Engineering-${suffix}`
const rootB = `Acceptance-Operations-${suffix}`
const child = `Platform-Team-${suffix}`
const renamedChild = `Platform-Engineering-${suffix}`

await navigate("/iam/departments")
await waitFor(`location.pathname === "/login" && document.querySelector("#login-name")`)
await setInput("#login-name", "admin@chaosplus.local")
await setInput("#password", password)
await evaluate(`
  const form = document.querySelector("form");
  if (!(form instanceof HTMLFormElement)) throw new Error("login form not found");
  form.requestSubmit();
`)
await waitFor(`location.pathname === "/iam/departments"`, 20000)
await waitFor(`document.querySelector("h1") && document.querySelector("tbody")`)
await cleanupArtifacts()

await createDepartment(rootA, 10)
await createDepartment(rootB, 20)
await createDepartment(child, 1, rootA)

let state = await departments()
const rootARecord = state.find((item) => item.name === rootA)
const rootBRecord = state.find((item) => item.name === rootB)
const childRecord = state.find((item) => item.name === child)
if (!rootARecord || !rootBRecord || !childRecord)
  throw new Error("created department records were not returned")
if (childRecord.parent_id !== rootARecord.id || childRecord.depth !== 1)
  throw new Error("created child hierarchy is incorrect")

await evaluate(`
  const row = ${rowExpression(rootA)};
  const button = row?.querySelector("button");
  if (!(button instanceof HTMLButtonElement)) throw new Error("collapse button not found");
  button.click();
`)
await waitFor(`!(${rowExpression(child)})`)
await evaluate(`
  const row = ${rowExpression(rootA)};
  const button = row?.querySelector("button");
  if (!(button instanceof HTMLButtonElement)) throw new Error("expand button not found");
  button.click();
`)
await waitFor(rowExpression(child))

await clickRowAction(rootA, 1)
await waitFor(`document.querySelector('[role="alert"]')`)

await clickRowAction(child, 2)
await waitFor(`document.querySelector("#department-form")`)
await setInput("#department-name", renamedChild)
await setInput("#department-sort-order", "2")
await selectOption("#department-parent", rootB)
await selectOption("#department-status", "停用")
await submitDepartmentForm()
await waitFor(rowExpression(renamedChild))

state = await departments()
const updatedChild = state.find((item) => item.id === childRecord.id)
if (
  !updatedChild ||
  updatedChild.name !== renamedChild ||
  updatedChild.parent_id !== rootBRecord.id ||
  updatedChild.status !== "disabled" ||
  updatedChild.depth !== 1 ||
  updatedChild.version !== childRecord.version + 1
)
  throw new Error("moved department state is incorrect")

const desktopLayout = await screenshot("admin-departments-desktop.png", 1440, 1000)
const mobileLayout = await screenshot("admin-departments-mobile.png", 390, 844)
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})

await clickRowAction(renamedChild, 1)
await waitFor(`!(${rowExpression(renamedChild)})`)
await clickRowAction(rootA, 1)
await waitFor(`!(${rowExpression(rootA)})`)
await clickRowAction(rootB, 1)
await waitFor(`!(${rowExpression(rootB)})`)

const audit = await evaluate(`
  const query = new URLSearchParams({ target_id: ${JSON.stringify(childRecord.id)}, limit: "100" });
  const response = await fetch("/api/iam/audit-events?" + query, {
    credentials: "include",
    headers: { "X-Tenant-Id": localStorage.getItem("chaosplus.tenant") || "platform" },
  });
  if (!response.ok) throw new Error("audit list failed: " + response.status);
  return (await response.json()).data;
`)
const childEvents = new Set(audit.map((event) => event.event_type))
for (const eventType of [
  "department_created",
  "department_updated",
  "department_deleted",
])
  if (!childEvents.has(eventType))
    throw new Error(`missing audit event: ${eventType}`)

const expectedConflicts = responses.filter(
  (response) =>
    response.status === 409 &&
    response.method === "DELETE" &&
    response.url.includes(`/api/iam/departments/${rootARecord.id}`)
)
if (expectedConflicts.length !== 1)
  throw new Error("parent deletion did not produce exactly one HTTP 409")

const unexpectedStatuses = responses.filter(
  (response) =>
    response.status >= 400 &&
    !(
      response.status === 401 &&
      response.url.endsWith("/api/authn/session")
    ) &&
    !expectedConflicts.includes(response)
)
const unexpectedFailures = failedRequests.filter(
  (request) =>
    !request.canceled && request.error !== "net::ERR_ABORTED"
)
socket.close()

if (consoleErrors.length)
  throw new Error(`console errors: ${JSON.stringify(consoleErrors)}`)
if (unexpectedFailures.length)
  throw new Error(`failed requests: ${JSON.stringify(unexpectedFailures)}`)
if (unexpectedStatuses.length)
  throw new Error(`unexpected HTTP statuses: ${JSON.stringify(unexpectedStatuses)}`)

console.log(
  JSON.stringify({
    anonymousDeepLinkReturned: true,
    hierarchyCreated: true,
    collapsedAndExpanded: true,
    movedRenamedAndDisabled: true,
    parentDeleteConflict: true,
    cleanupCompleted: true,
    auditEvents: [...childEvents].sort(),
    desktopLayout,
    mobileLayout,
    consoleErrors,
    failedRequests: unexpectedFailures,
    unexpectedStatuses,
  })
)
