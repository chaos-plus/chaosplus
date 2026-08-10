const port = Number(process.argv[2] ?? 9333)
const baseURL = process.argv[3] ?? "http://127.0.0.1:8091"
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
const requests = []
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
    requests.push({
      method: message.params.request.method,
      url: message.params.request.url,
      headers: message.params.request.headers,
    })
  if (message.method === "Network.responseReceived")
    responses.push({
      url: message.params.response.url,
      status: message.params.response.status,
    })
  if (message.method === "Network.loadingFailed")
    failedRequests.push({
      url: message.params.requestId,
      error: message.params.errorText,
      canceled: Boolean(message.params.canceled),
    })
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
  const url = new URL(path, baseURL).href
  await command("Page.navigate", { url })
  await waitFor(
    `location.origin === ${JSON.stringify(new URL(baseURL).origin)} && document.readyState === "complete"`
  )
}

async function setInput(selector, value) {
  await evaluate(`
    const input = document.querySelector(${JSON.stringify(selector)});
    if (!(input instanceof HTMLInputElement)) throw new Error("input not found");
    const setter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input), "value").set;
    setter.call(input, ${JSON.stringify(value)});
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  `)
}

async function click(selector) {
  await evaluate(`
    const element = document.querySelector(${JSON.stringify(selector)});
    if (!(element instanceof HTMLElement)) throw new Error("element not found: " + ${JSON.stringify(selector)});
    element.click();
  `)
}

async function clickText(text, root = "document") {
  await evaluate(`
    const element = [...${root}.querySelectorAll("button")].find(
      (item) => item.textContent.trim() === ${JSON.stringify(text)}
    );
    if (!(element instanceof HTMLButtonElement)) throw new Error("button not found: " + ${JSON.stringify(text)});
    element.click();
  `)
}

async function selectOption(selector, label) {
  await click(selector)
  await waitFor(`document.querySelector('[role="option"]')`)
  const point = await evaluate(`
    const option = [...document.querySelectorAll('[role="option"]')].find(
      (item) => item.textContent.includes(${JSON.stringify(label)})
    );
    if (!(option instanceof HTMLElement)) throw new Error("option not found");
    const rect = option.getBoundingClientRect();
    return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
  `)
  for (const type of ["mousePressed", "mouseReleased"])
    await command("Input.dispatchMouseEvent", {
      type,
      x: point.x,
      y: point.y,
      button: "left",
      clickCount: 1,
    })
}

async function submit(formID) {
  await evaluate(`
    const form = document.querySelector(${JSON.stringify(`#${formID}`)});
    if (!(form instanceof HTMLFormElement)) throw new Error("form not found");
    form.requestSubmit();
  `)
}

async function login(loginName, loginPassword) {
  await waitFor(
    `location.pathname === "/login" && document.querySelector("#login-name")`
  )
  await setInput("#login-name", loginName)
  await setInput("#password", loginPassword)
  await evaluate(`document.querySelector("form").requestSubmit()`)
  await waitFor(`location.pathname === "/iam/tenants"`, 20000)
}

async function api(path, init = {}, tenant = "platform") {
  return evaluate(`
    const headers = { "Content-Type": "application/json" };
    if (${JSON.stringify(tenant)} !== "") headers["X-Tenant-Id"] = ${JSON.stringify(tenant)};
    const response = await fetch(${JSON.stringify(`/api${path}`)}, {
      ...${JSON.stringify(init)}, headers, credentials: "include"
    });
    const body = await response.json();
    if (!response.ok) throw new Error(body.message || ("HTTP " + response.status));
    return body.data;
  `)
}

function rowExpression(name) {
  return `[...document.querySelectorAll("tbody tr")].find((row) => [...row.querySelectorAll("strong")].some((item) => item.textContent.trim() === ${JSON.stringify(name)}))`
}

async function clickRowAction(name, title) {
  await evaluate(`
    const row = ${rowExpression(name)};
    const button = [...row.querySelectorAll("button")].find((item) => item.title === ${JSON.stringify(title)});
    if (!(button instanceof HTMLButtonElement)) throw new Error("row action not found");
    button.click();
  `)
}

async function capture(name, width, height) {
  await command("Emulation.setDeviceMetricsOverride", {
    width,
    height,
    deviceScaleFactor: 1,
    mobile: width < 600,
  })
  await Bun.sleep(250)
  const layout = await evaluate(`return {
    width: innerWidth,
    scrollWidth: document.documentElement.scrollWidth,
    horizontalOverflow: document.documentElement.scrollWidth > innerWidth,
  }`)
  if (layout.horizontalOverflow)
    throw new Error(`Horizontal overflow at ${width}px`)
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
await navigate("/iam/tenants")
await login("admin@chaosplus.local", password)
await waitFor(
  `document.querySelector('a[href="/iam/tenants"]') && document.querySelector("tbody")`
)

const administrator = (await api("/iam/roles")).find(
  (role) => role.name === "System Administrator"
)
if (!administrator) throw new Error("built-in administrator role was not found")
const previousOperators = await api(
  "/iam/principals?limit=200&search=acceptance-tenant-admin"
)
for (const principal of previousOperators.items) {
  if (principal.status !== "active") continue
  await api(
    `/iam/roles/${encodeURIComponent(administrator.id)}/members/${encodeURIComponent(principal.id)}`,
    { method: "DELETE" }
  )
  await api(`/iam/members/${encodeURIComponent(principal.id)}`, {
    method: "PATCH",
    body: JSON.stringify({ status: "disabled" }),
  })
  await api(`/iam/principals/${encodeURIComponent(principal.id)}/disable`, {
    method: "POST",
  })
}

const suffix = Date.now().toString(36)
const operatorLogin = `acceptance-tenant-admin-${suffix}`
const operatorPassword = "Chaosplus-Acceptance-2026!"
const operator = await api("/iam/principals", {
  method: "POST",
  body: JSON.stringify({
    login_name: operatorLogin,
    password: operatorPassword,
    display_name: `Tenant Admin ${suffix}`,
  }),
})
await api("/iam/members", {
  method: "POST",
  body: JSON.stringify({
    subject: operator.id,
    display_name: operator.display_name,
    status: "active",
  }),
})
await api(
  `/iam/roles/${encodeURIComponent(administrator.id)}/members/${encodeURIComponent(operator.id)}`,
  {
    method: "PUT",
  }
)

await api("/authn/logout", { method: "POST" }, "")
await command("Network.clearBrowserCookies")
await navigate("/iam/tenants")
await login(operatorLogin, operatorPassword)
await waitFor(`document.querySelector('[role="alert"]')`)
if (
  await evaluate(
    `return Boolean(document.querySelector('a[href="/iam/tenants"]'))`
  )
)
  throw new Error("tenant administrator can see platform navigation")

await api("/authn/logout", { method: "POST" }, "")
await command("Network.clearBrowserCookies")
await navigate("/iam/tenants")
await login("admin@chaosplus.local", password)
await waitFor(
  `document.querySelector('a[href="/iam/tenants"]') && document.querySelector("tbody")`
)

const slug = `acceptance-${suffix}`
const name = `Acceptance Tenant ${suffix}`
const updatedName = `${name} Updated`
await clickText("创建租户")
await waitFor(`document.querySelector("#tenant-form")`)
await setInput("#tenant-name", name)
await setInput("#tenant-slug", slug)
await submit("tenant-form")
await waitFor(`!document.querySelector("#tenant-form")`)
await waitFor(rowExpression(name))

let tenant = (await api("/iam/tenants", {}, "")).find(
  (item) => item.slug === slug
)
if (!tenant) throw new Error("created tenant was not returned")

await clickRowAction(name, "编辑租户")
await waitFor(`document.querySelector("#tenant-form")`)
await setInput("#tenant-name", updatedName)
await selectOption("#tenant-status", "停用")
await submit("tenant-form")
await waitFor(`!document.querySelector("#tenant-form")`)
await waitFor(`${rowExpression(updatedName)}?.textContent.includes("停用")`)

await clickRowAction(updatedName, "编辑租户")
await waitFor(`document.querySelector("#tenant-form")`)
await selectOption("#tenant-status", "启用")
await submit("tenant-form")
await waitFor(`!document.querySelector("#tenant-form")`)
await waitFor(`${rowExpression(updatedName)}?.textContent.includes("启用")`)

const layouts = [
  await capture("admin-tenants-desktop.png", 1440, 1000),
  await capture("admin-tenants-mobile.png", 390, 844),
]
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})

await clickRowAction(updatedName, "进入租户")
await waitFor(
  `location.pathname === "/" && localStorage.getItem("chaosplus.tenant") === ${JSON.stringify(tenant.id)}`
)
await evaluate(`
  localStorage.setItem("chaosplus.tenant", "platform");
  window.dispatchEvent(new Event("tenant-change"));
`)
await navigate("/iam/tenants")
await waitFor(rowExpression(updatedName))

tenant = (await api("/iam/tenants", {}, "")).find(
  (item) => item.id === tenant.id
)
await clickRowAction(updatedName, "删除租户")
await waitFor(`document.querySelector('[role="dialog"]')`)
await clickText("确认删除")
await waitFor(`!(${rowExpression(updatedName)})`)
await click("#include-deleted-tenants")
await waitFor(`${rowExpression(updatedName)}?.textContent.includes("已删除")`)

await api(
  `/iam/roles/${encodeURIComponent(administrator.id)}/members/${encodeURIComponent(operator.id)}`,
  {
    method: "DELETE",
  }
)
await api(`/iam/members/${encodeURIComponent(operator.id)}`, {
  method: "PATCH",
  body: JSON.stringify({ status: "disabled" }),
})
await api(`/iam/principals/${encodeURIComponent(operator.id)}/disable`, {
  method: "POST",
})

const tenantRequests = requests.filter((request) =>
  request.url.includes("/api/iam/tenants")
)
if (tenantRequests.length < 8)
  throw new Error(`tenant lifecycle requests missing: ${tenantRequests.length}`)
for (const request of tenantRequests) {
  const hasTenantHeader = Object.keys(request.headers).some(
    (name) => name.toLowerCase() === "x-tenant-id"
  )
  if (hasTenantHeader)
    throw new Error(
      `platform tenant request leaked tenant context: ${request.url}`
    )
}

const unexpectedResponses = responses.filter(
  (response) => response.status >= 400 && ![401, 403].includes(response.status)
)
const unexpectedFailures = failedRequests.filter((request) => !request.canceled)
socket.close()
if (consoleErrors.length > 0)
  throw new Error(`console errors: ${consoleErrors.join("; ")}`)
if (unexpectedFailures.length > 0)
  throw new Error(`failed requests: ${JSON.stringify(unexpectedFailures)}`)
if (unexpectedResponses.length > 0)
  throw new Error(
    `unexpected responses: ${JSON.stringify(unexpectedResponses)}`
  )

console.log(
  JSON.stringify(
    {
      tenantID: tenant.id,
      operatorID: operator.id,
      layouts,
      tenantRequests: tenantRequests.length,
      consoleErrors,
      failedRequests: unexpectedFailures,
      unexpectedResponses,
    },
    null,
    2
  )
)
