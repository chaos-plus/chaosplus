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

async function scim(path, method = "GET", body, token = "", headers = {}) {
  return evaluate(`
    const headers = ${JSON.stringify(headers)};
    if (${JSON.stringify(token)}) headers.Authorization = "Bearer " + ${JSON.stringify(token)};
    if (${body === undefined ? "false" : "true"}) headers["Content-Type"] = "application/scim+json";
    const response = await fetch(${JSON.stringify(`/api/scim/v2${path}`)}, {
      method: ${JSON.stringify(method)},
      headers,
      body: ${body === undefined ? "undefined" : JSON.stringify(JSON.stringify(body))},
    });
    let body = null;
    try { body = await response.json(); } catch {}
    return {
      status: response.status,
      body,
      contentType: response.headers.get("content-type"),
      etag: response.headers.get("etag"),
      location: response.headers.get("location"),
    };
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
await command("Browser.grantPermissions", {
  origin: new URL(baseURL).origin,
  permissions: ["clipboardReadWrite", "clipboardSanitizedWrite"],
})

await navigate("/iam/scim-directories")
await waitFor(
  `location.pathname === "/login" && document.querySelector("#login-name")`
)
await evaluate(`localStorage.setItem("chaosplus.tenant", "platform")`)
await setInput("#login-name", "admin@chaosplus.local")
await setInput("#password", password)
await submit("form")
await waitFor(`location.pathname === "/iam/scim-directories"`, 20000)
await waitFor(
  `document.querySelector("h1")?.textContent.includes("SCIM 目录") && document.querySelector("tbody")`
)
await waitFor(`document.querySelector('a[href="/iam/scim-directories"]')`)

await clickLabel("复制 SCIM Base URL")
await waitFor(`document.body.innerText.includes("SCIM Base URL 已复制")`)
const copiedEndpoint = await evaluate(`return navigator.clipboard.readText()`)
if (!copiedEndpoint.endsWith("/api/scim/v2"))
  throw new Error(`unexpected copied endpoint: ${copiedEndpoint}`)

const suffix = Date.now().toString(36)
const directoryName = `Browser SCIM ${suffix}`
const renamedName = `${directoryName} Updated`
const credentialName = `browser-${suffix}`

await clickText("创建目录")
await waitFor(`document.querySelector("#scim-directory-form")`)
await setInput("#scim-directory-name", directoryName)
await submit("#scim-directory-form")
await waitFor(rowExpression(directoryName))

await clickLabel(`编辑 ${directoryName}`)
await waitFor(`document.querySelector("#scim-directory-enabled")`)
await setInput("#scim-directory-name", renamedName)
await evaluate(`document.querySelector("#scim-directory-enabled").click()`)
await submit("#scim-directory-form")
await waitFor(`${rowExpression(renamedName)}?.textContent.includes("停用")`)

await clickLabel(`编辑 ${renamedName}`)
await evaluate(`document.querySelector("#scim-directory-enabled").click()`)
await submit("#scim-directory-form")
await waitFor(`${rowExpression(renamedName)}?.textContent.includes("启用")`)

await clickLabel(`管理 ${renamedName} 的凭据`)
await waitFor(`document.querySelector("#scim-credential-form")`)
await setInput("#scim-credential-name", credentialName)
await setInput(
  "#scim-credential-expiry",
  new Date(Date.now() + 86400000).toISOString().slice(0, 16)
)
await submit("#scim-credential-form")
await waitFor(
  `document.querySelector('[role="dialog"]')?.textContent.includes("token 仅显示一次")`
)
const secret = await evaluate(`
  const inputs = [...document.querySelectorAll('[role="dialog"] input')];
  return { endpoint: inputs[0]?.value, token: inputs[1]?.value };
`)
if (
  !secret.endpoint.endsWith("/api/scim/v2") ||
  !secret.token.startsWith("scim_")
)
  throw new Error("one-time SCIM credential was not rendered")

const discovery = await scim("/ServiceProviderConfig")
if (
  discovery.status !== 200 ||
  !discovery.contentType?.startsWith("application/scim+json") ||
  discovery.body?.patch?.supported !== true
)
  throw new Error(`SCIM discovery failed: ${JSON.stringify(discovery)}`)

const user = await scim(
  "/Users",
  "POST",
  {
    schemas: ["urn:ietf:params:scim:schemas:core:2.0:User"],
    externalId: `browser-user-${suffix}`,
    userName: `browser-scim-${suffix}@example.test`,
    displayName: `Browser SCIM User ${suffix}`,
    active: true,
    emails: [{ value: `browser-scim-${suffix}@example.test`, primary: true }],
  },
  secret.token
)
if (user.status !== 201 || !user.body?.id || !user.etag)
  throw new Error(`SCIM user create failed: ${JSON.stringify(user)}`)

const group = await scim(
  "/Groups",
  "POST",
  {
    schemas: ["urn:ietf:params:scim:schemas:core:2.0:Group"],
    externalId: `browser-group-${suffix}`,
    displayName: `Browser SCIM Group ${suffix}`,
    members: [{ value: user.body.id }],
  },
  secret.token
)
if (
  group.status !== 201 ||
  group.body?.members?.[0]?.value !== user.body.id ||
  !group.etag
)
  throw new Error(`SCIM group create failed: ${JSON.stringify(group)}`)

const deletedGroup = await scim(
  `/Groups/${encodeURIComponent(group.body.id)}`,
  "DELETE",
  undefined,
  secret.token,
  { "If-Match": group.etag }
)
const deletedUser = await scim(
  `/Users/${encodeURIComponent(user.body.id)}`,
  "DELETE",
  undefined,
  secret.token,
  { "If-Match": user.etag }
)
if (deletedGroup.status !== 204 || deletedUser.status !== 204)
  throw new Error(
    `SCIM deprovision failed: ${JSON.stringify({ deletedGroup, deletedUser })}`
  )

await clickText("已安全保存")
await clickLabel(`管理 ${renamedName} 的凭据`)
await waitFor(
  `document.body.innerText.includes(${JSON.stringify(credentialName)})`
)
await clickLabel(`撤销 ${credentialName}`)
await clickText("确认撤销")
await waitFor(`document.body.innerText.includes("已撤销")`)
await clickText("关闭")

const revoked = await scim("/Users", "GET", undefined, secret.token, {
  "Accept-Language": "ms-MY",
})
if (
  revoked.status !== 401 ||
  revoked.body?.detail === "scim_unauthorized" ||
  !revoked.body?.detail
)
  throw new Error(`revoked SCIM token was accepted: ${JSON.stringify(revoked)}`)

await clickLabel(`编辑 ${renamedName}`)
await evaluate(`document.querySelector("#scim-directory-enabled").click()`)
await submit("#scim-directory-form")
await waitFor(`${rowExpression(renamedName)}?.textContent.includes("停用")`)

const layouts = [
  await capture("admin-scim-desktop.png", 1440, 1000),
  await capture("admin-scim-mobile.png", 390, 844),
]

const expectedUnauthorized = responses.find(
  (response) =>
    response.status === 401 && response.url.includes("/api/scim/v2/Users")
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
  throw new Error(
    `unexpected responses: ${JSON.stringify(unexpectedResponses)}`
  )

console.log(
  JSON.stringify({
    deepLinkLogin: true,
    directoryLifecycle: true,
    endpointClipboard: true,
    oneTimeCredential: true,
    userAndGroupProvisioning: true,
    deprovisioning: true,
    revokedTokenRejected: true,
    localizedProtocolError: true,
    layouts,
    consoleErrors,
    unexpectedFailures,
    unexpectedResponses,
  })
)
