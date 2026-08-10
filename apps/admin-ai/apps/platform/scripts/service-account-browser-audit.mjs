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

async function setInput(selector, value) {
  await evaluate(`
    const input = document.querySelector(${JSON.stringify(selector)});
    if (!(input instanceof HTMLInputElement)) throw new Error("input not found: " + ${JSON.stringify(selector)});
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
    setter.call(input, ${JSON.stringify(value)});
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  `)
}

async function clickText(text) {
  await evaluate(`
    const button = [...document.querySelectorAll("button")].find(
      (item) => item.textContent.trim() === ${JSON.stringify(text)} && item.getClientRects().length
    );
    if (!(button instanceof HTMLButtonElement)) throw new Error("button not found: " + ${JSON.stringify(text)});
    button.click();
  `)
}

async function clickLabel(label) {
  await evaluate(`
    const button = document.querySelector('button[aria-label="' + CSS.escape(${JSON.stringify(label)}) + '"]');
    if (!(button instanceof HTMLButtonElement)) throw new Error("button not found: " + ${JSON.stringify(label)});
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

async function api(path, method = "GET", body, authorization = "") {
  const result = await evaluate(`
    const headers = { "Content-Type": "application/json", "X-Tenant-Id": "platform" };
    if (${JSON.stringify(authorization)}) headers.Authorization = ${JSON.stringify(authorization)};
    const response = await fetch(${JSON.stringify(`/api${path}`)}, {
      method: ${JSON.stringify(method)},
      headers,
      credentials: "include",
      body: ${body === undefined ? "undefined" : JSON.stringify(JSON.stringify(body))},
    });
    let body = null;
    try { body = await response.json(); } catch {}
    return { status: response.status, body };
  `)
  return result
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

function rowExpression(name) {
  return `[...document.querySelectorAll("tbody tr")].find((row) => row.querySelector("strong")?.textContent.trim() === ${JSON.stringify(name)})`
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

await navigate("/iam/service-accounts")
await waitFor(
  `location.pathname === "/login" && document.querySelector("#login-name")`
)
await evaluate(`localStorage.setItem("chaosplus.tenant", "platform")`)
await setInput("#login-name", "admin@chaosplus.local")
await setInput("#password", password)
await submit("form")
await waitFor(`location.pathname === "/iam/service-accounts"`, 20000)
await waitFor(
  `document.querySelector("h1")?.textContent.includes("服务账号") && document.querySelector("tbody")`
)
await waitFor(`document.querySelector('a[href="/iam/service-accounts"]')`)

const suffix = Date.now().toString(36)
const loginName = `audit-worker-${suffix}`
const displayName = `Audit Worker ${suffix}`
const credentialName = `audit-${suffix}`

await clickText("创建服务账号")
await waitFor(`document.querySelector("#service-account-form")`)
await setInput("#service-account-login", loginName)
await setInput("#service-account-display", displayName)
await submit("#service-account-form")
await waitFor(rowExpression(displayName))
const accountID = await evaluate(`return ${rowExpression(displayName)}.querySelector("code").textContent.trim()`)

const role = await api("/iam/roles", "POST", {
  name: `Service Account Audit ${suffix}`,
  description: "Browser acceptance role",
})
if (role.status !== 200) throw new Error(`role create failed: ${JSON.stringify(role)}`)
const roleID = role.body.data.id
for (const path of [
  `/iam/roles/${encodeURIComponent(roleID)}/permissions/service_account_view`,
  `/iam/roles/${encodeURIComponent(roleID)}/members/${encodeURIComponent(accountID)}`,
]) {
  const assigned = await api(path, "PUT")
  if (assigned.status !== 200)
    throw new Error(`role assignment failed: ${JSON.stringify(assigned)}`)
}

await clickLabel(`管理 ${displayName} 的凭据`)
await waitFor(`document.querySelector("#service-credential-form")`)
await setInput("#service-credential-name", credentialName)
await setInput("#service-credential-scopes", "chaosplus-api")
await submit("#service-credential-form")
await waitFor(`document.querySelector('[role="dialog"]')?.textContent.includes("客户端密钥仅显示一次")`)
const secret = await evaluate(`
  const inputs = [...document.querySelectorAll('[role="dialog"] input')];
  return { clientID: inputs[0].value, clientSecret: inputs[1].value };
`)
if (!secret.clientID.startsWith("sac_") || !secret.clientSecret)
  throw new Error("one-time client credential was not rendered")

const token = await evaluate(`
  const form = new URLSearchParams({
    grant_type: "client_credentials",
    client_id: ${JSON.stringify(secret.clientID)},
    client_secret: ${JSON.stringify(secret.clientSecret)},
    scope: "chaosplus-api",
  });
  const response = await fetch("/api/oauth/token", {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: form,
  });
  return { status: response.status, body: await response.json() };
`)
if (token.status !== 200 || !token.body.access_token)
  throw new Error(`token exchange failed: ${JSON.stringify(token)}`)
const authorized = await api(
  "/iam/service-accounts",
  "GET",
  undefined,
  `Bearer ${token.body.access_token}`
)
if (authorized.status !== 200)
  throw new Error(`service account RBAC failed: ${JSON.stringify(authorized)}`)

await clickText("完成")
await clickLabel(`管理 ${displayName} 的凭据`)
await waitFor(`document.body.innerText.includes(${JSON.stringify(credentialName)})`)
await clickLabel(`撤销 ${credentialName}`)
await clickText("确认")
await waitFor(`document.querySelector('[role="dialog"]')?.textContent.includes("已撤销")`)
const revoked = await api(
  "/iam/service-accounts",
  "GET",
  undefined,
  `Bearer ${token.body.access_token}`
)
if (revoked.status !== 401)
  throw new Error(`revoked token remained valid: ${JSON.stringify(revoked)}`)
await clickText("关闭")

await clickLabel(`编辑 ${displayName}`)
await waitFor(`document.querySelector("#service-account-enabled")`)
await evaluate(`document.querySelector("#service-account-enabled").click()`)
await submit("#service-account-form")
await waitFor(`${rowExpression(displayName)}?.textContent.includes("停用")`)
await clickLabel(`编辑 ${displayName}`)
await evaluate(`document.querySelector("#service-account-enabled").click()`)
await submit("#service-account-form")
await waitFor(`${rowExpression(displayName)}?.textContent.includes("启用")`)

const layouts = [
  await capture("admin-service-accounts-desktop.png", 1440, 1000),
  await capture("admin-service-accounts-mobile.png", 390, 844),
]
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})
await clickLabel(`删除 ${displayName}`)
await clickText("确认")
await waitFor(`!${rowExpression(displayName)}`)
const deletedRole = await api(`/iam/roles/${encodeURIComponent(roleID)}`, "DELETE")
if (deletedRole.status !== 200)
  throw new Error(`role cleanup failed: ${JSON.stringify(deletedRole)}`)

const expectedUnauthorized = responses.find(
  (response) =>
    response.status === 401 &&
    response.method === "GET" &&
    response.url.includes("/api/iam/service-accounts")
)
const unexpectedResponses = responses.filter(
  (response) =>
    response.status >= 400 &&
    response !== expectedUnauthorized &&
    !(response.status === 401 && response.url.endsWith("/api/authn/session"))
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
  throw new Error(`unexpected responses: ${JSON.stringify(unexpectedResponses)}`)

console.log(
  JSON.stringify({
    accountLifecycle: true,
    oneTimeCredential: true,
    clientCredentials: true,
    rbacAuthorized: true,
    revokedTokenRejected: true,
    layouts,
    consoleErrors,
    unexpectedFailures,
    unexpectedResponses,
  })
)
