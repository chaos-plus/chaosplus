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
const requestByID = new Map()
const responses = []
const failures = []
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
    failures.push({
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
  await command("Page.navigate", { url: new URL(path, baseURL).href })
  await waitFor(`document.readyState === "complete"`)
}

async function setControl(selector, value) {
  await evaluate(`
    const control = document.querySelector(${JSON.stringify(selector)});
    if (!(control instanceof HTMLInputElement) && !(control instanceof HTMLTextAreaElement))
      throw new Error("control not found: " + ${JSON.stringify(selector)});
    const prototype = control instanceof HTMLInputElement ? HTMLInputElement.prototype : HTMLTextAreaElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, "value").set.call(control, ${JSON.stringify(value)});
    control.dispatchEvent(new Event("input", { bubbles: true }));
    control.dispatchEvent(new Event("change", { bubbles: true }));
  `)
}

async function clickText(text) {
  await evaluate(`
    const element = [...document.querySelectorAll("button, [role=tab], [role=option]")]
      .find((item) => item.textContent.trim() === ${JSON.stringify(text)});
    if (!(element instanceof HTMLElement)) throw new Error("control not found: " + ${JSON.stringify(text)});
    element.click();
  `)
}

async function clickTitle(title, rowText = "") {
  await evaluate(`
    const rows = [...document.querySelectorAll("tbody tr")];
    const root = ${JSON.stringify(rowText)} ? rows.find((row) => row.textContent.includes(${JSON.stringify(rowText)})) : document;
    const button = root?.querySelector(${JSON.stringify(`button[title="${title}"]`)});
    if (!(button instanceof HTMLButtonElement)) throw new Error("button not found: " + ${JSON.stringify(title)});
    button.click();
  `)
}

async function submit(selector) {
  await evaluate(`
    const form = document.querySelector(${JSON.stringify(selector)});
    if (!(form instanceof HTMLFormElement)) throw new Error("form not found");
    form.requestSubmit();
  `)
}

async function api(path, options = {}) {
  const result = await evaluate(`
    const response = await fetch(${JSON.stringify(`/api${path}`)}, {
      credentials: "include",
      headers: { "Content-Type": "application/json", "X-Tenant-Id": "platform" },
      ...${JSON.stringify(options)},
    });
    const payload = await response.json();
    return { status: response.status, payload };
  `)
  if (result.status < 200 || result.status >= 300)
    throw new Error(
      `${path} failed: ${result.status} ${result.payload?.message ?? ""}`
    )
  return result.payload.data
}

async function login(loginName, loginPassword) {
  await navigate("/")
  await waitFor(
    `location.pathname === "/login" && document.querySelector("#login-name")`
  )
  await evaluate(`localStorage.setItem("chaosplus.tenant", "platform")`)
  await setControl("#login-name", loginName)
  await setControl("#password", loginPassword)
  await submit("form")
  await waitFor(`location.pathname === "/"`, 20000)
}

async function logout() {
  const status = await evaluate(`
	  const response = await fetch("/api/authn/logout", { method: "POST", credentials: "include" });
	  return response.status;
	`)
  if (status < 200 || status >= 300) throw new Error(`logout failed: ${status}`)
  await command("Network.clearBrowserCookies")
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
    heading: document.querySelector("h1")?.textContent ?? "",
  }`)
  if (layout.horizontalOverflow || !layout.heading.includes("访问申请"))
    throw new Error(`${name} layout failed: ${JSON.stringify(layout)}`)
  const screenshot = await command("Page.captureScreenshot", {
    format: "png",
    fromSurface: true,
    captureBeyondViewport: false,
  })
  await Bun.write(
    new URL(name, screenshotRoot),
    Buffer.from(screenshot.data, "base64")
  )
  return layout
}

await Promise.all([
  command("Network.enable"),
  command("Page.enable"),
  command("Runtime.enable"),
])
await command("Runtime.discardConsoleEntries")
await command("Network.clearBrowserCookies")
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})

const suffix = Date.now().toString(36)
const roleName = `Acceptance Access ${suffix}`
const approverLogin = `access-approver-${suffix}`
const approverEmail = `${approverLogin}@example.test`
const approverPassword = "Chaosplus-Access-Approver-2026!"

await login("admin@chaosplus.local", password)
for (const stale of (await api("/iam/roles")).filter((item) =>
  item.name.startsWith("Acceptance Access ")
))
  await api(`/iam/roles/${encodeURIComponent(stale.id)}`, {
    method: "DELETE",
  })
const tenantRoles = await api("/iam/roles")
let administratorRole
for (const candidate of tenantRoles) {
  const permissions = await api(
    `/iam/roles/${encodeURIComponent(candidate.id)}/permissions`
  )
  if (permissions.includes("tenant_administer")) {
    administratorRole = candidate
    break
  }
}
if (!administratorRole)
  throw new Error("tenant administrator role was not found")
const role = await api("/iam/roles", {
  method: "POST",
  body: JSON.stringify({
    name: roleName,
    description: "Browser acceptance role",
  }),
})
const invitation = await api("/iam/invitations", {
  method: "POST",
  body: JSON.stringify({
    email: approverEmail,
    role_ids: [administratorRole.id],
  }),
})
const acceptance = await api("/iam/invitations/accept", {
  method: "POST",
  body: JSON.stringify({
    token: invitation.token,
    login_name: approverLogin,
    password: approverPassword,
    display_name: `Access Approver ${suffix}`,
  }),
})

await navigate("/iam/access-requests")
await waitFor(`document.querySelector("h1")?.textContent.includes("访问申请")`)
await waitFor(
  `[...document.querySelectorAll("button")].some((button) => button.textContent.trim() === "发起申请" && !button.disabled)`
)
await clickText("发起申请")
await waitFor(`document.querySelector("#access-request-form")`)
await evaluate(`document.querySelector("#access-request-role").click()`)
await waitFor(
  `[...document.querySelectorAll("[role=option]")].some((item) => item.textContent.trim() === ${JSON.stringify(roleName)})`
)
await clickText(roleName)
await setControl(
  "#access-request-expiry",
  new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString().slice(0, 16)
)
await setControl("#access-request-reason", "Browser acceptance access request")
await submit("#access-request-form")
await waitFor(
  `[...document.querySelectorAll("tbody tr")].some((row) => row.textContent.includes(${JSON.stringify(roleName)}) && row.textContent.includes("待审批"))`
)

await logout()
await login(approverLogin, approverPassword)
await navigate("/iam/access-requests")
await waitFor(
  `[...document.querySelectorAll("[role=tab]")].some((tab) => tab.textContent.trim() === "审批队列")`
)
await clickText("审批队列")
await waitFor(
  `[...document.querySelectorAll("tbody tr")].some((row) => row.textContent.includes(${JSON.stringify(roleName)}))`
)
await clickTitle("批准申请", roleName)
await waitFor(`document.querySelector("#access-decision-form")`)
await setControl("#access-decision-note", "Approved by browser acceptance")
await submit("#access-decision-form")
await waitFor(
  `[...document.querySelectorAll("tbody tr")].some((row) => row.textContent.includes(${JSON.stringify(roleName)}) && row.textContent.includes("已批准"))`
)

const layouts = [
  await capture("admin-access-requests-desktop.png", 1440, 1000),
  await capture("admin-access-requests-mobile.png", 390, 844),
]

await logout()
await login("admin@chaosplus.local", password)
await navigate("/iam/access-requests")
await waitFor(
  `[...document.querySelectorAll("tbody tr")].some((row) => row.textContent.includes(${JSON.stringify(roleName)}) && row.textContent.includes("已批准"))`
)
await clickTitle("放弃访问权限", roleName)
await waitFor(`document.querySelector("#access-decision-form")`)
await setControl("#access-decision-note", "Browser acceptance completed")
await submit("#access-decision-form")
await waitFor(
  `[...document.querySelectorAll("tbody tr")].some((row) => row.textContent.includes(${JSON.stringify(roleName)}) && row.textContent.includes("已撤销"))`
)

await api(
  `/iam/roles/${encodeURIComponent(administratorRole.id)}/members/${encodeURIComponent(acceptance.principal_id)}`,
  {
    method: "DELETE",
  }
)
await api(`/iam/roles/${encodeURIComponent(role.id)}`, { method: "DELETE" })

const governanceResponses = responses.filter((response) =>
  response.url.includes("/api/iam/access-requests")
)
for (const expected of ["GET", "POST"])
  if (
    !governanceResponses.some(
      (response) => response.method === expected && response.status < 400
    )
  )
    throw new Error(`missing successful ${expected} access request`)

const unexpectedResponses = responses.filter(
  (response) =>
    response.status >= 400 &&
    !(response.status === 401 && response.url.endsWith("/api/authn/session")) &&
    !(
      response.status === 403 &&
      response.url.includes("/api/iam/tenants?include_deleted=false")
    )
)
const unexpectedFailures = failures.filter(
  (failure) => !failure.canceled && failure.error !== "net::ERR_ABORTED"
)
socket.close()
if (consoleErrors.length)
  throw new Error(`console errors: ${JSON.stringify(consoleErrors)}`)
if (unexpectedFailures.length)
  throw new Error(`failed requests: ${JSON.stringify(unexpectedFailures)}`)
if (unexpectedResponses.length)
  throw new Error(
    `unexpected responses: ${JSON.stringify(unexpectedResponses)}`
  )

console.log(
  JSON.stringify({
    approved: true,
    relinquished: true,
    layouts,
    governanceResponses,
  })
)
