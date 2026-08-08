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
  if (message.method === "Page.javascriptDialogOpening")
    void command("Page.handleJavaScriptDialog", { accept: true })
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
    const button = [...document.querySelectorAll("button")].find((item) => item.textContent.trim() === ${JSON.stringify(text)});
    if (!(button instanceof HTMLButtonElement)) throw new Error("button not found: " + ${JSON.stringify(text)});
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

function rowExpression(email) {
  return `[...document.querySelectorAll("tbody tr")].find((row) => row.querySelector("strong")?.textContent.trim() === ${JSON.stringify(email)})`
}

async function createInvitation(email) {
  await clickText("创建邀请")
  await waitFor(`document.querySelector("#invitation-form")`)
  await setInput("#invitation-email", email)
  await submit("#invitation-form")
  try {
    await waitFor(`document.querySelector("#issued-invitation-token")`)
  } catch (cause) {
    const diagnostic = await evaluate(`return {
      body: document.body.innerText.slice(0, 3000),
      alert: document.querySelector('[role="alert"]')?.textContent ?? "",
      valid: document.querySelector("#invitation-form")?.checkValidity() ?? null,
    }`)
    const recent = responses
      .filter((response) => response.url.includes("/api/iam/invitations"))
      .slice(-8)
    throw new Error(
      `${cause.message}; diagnostic=${JSON.stringify(diagnostic)}; responses=${JSON.stringify(recent)}`
    )
  }
  const token = await evaluate(
    `return document.querySelector("#issued-invitation-token").value`
  )
  if (!token.startsWith("cpi1_"))
    throw new Error("issued invitation token is malformed")
  await clickText("完成")
  await waitFor(
    `!document.querySelector("#issued-invitation-token") && ${rowExpression(email)}`
  )
  return token
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

await navigate("/")
await waitFor(
  `location.pathname === "/login" && document.querySelector("#login-name")`
)
await evaluate(`localStorage.setItem("chaosplus.tenant", "platform")`)
await setInput("#login-name", "admin@chaosplus.local")
await setInput("#password", password)
await submit("form")
await waitFor(`location.pathname === "/"`, 20000)
await navigate("/iam/invitations")
try {
  await waitFor(
    `document.querySelector("h1")?.textContent.includes("成员邀请") && document.querySelector("tbody")`
  )
} catch (cause) {
  const diagnostic = await evaluate(`return {
    path: location.pathname,
    body: document.body.innerText.slice(0, 3000),
    html: document.body.innerHTML.slice(0, 1000),
  }`)
  throw new Error(`${cause.message}; diagnostic=${JSON.stringify(diagnostic)}`)
}
await waitFor(`document.querySelector('a[href="/iam/invitations"]')`)

const suffix = Date.now().toString(36)
const acceptedEmail = `invited-${suffix}@example.test`
const revokedEmail = `revoked-${suffix}@example.test`
const token = await createInvitation(acceptedEmail)
const oldRevokedToken = await createInvitation(revokedEmail)

await evaluate(`
  const row = ${rowExpression(revokedEmail)};
  const button = [...row.querySelectorAll("button")].find((item) => item.title === "重新发送并轮换凭据");
  if (!(button instanceof HTMLButtonElement)) throw new Error("resend button not found");
  button.click();
`)
await waitFor(`document.querySelector("#issued-invitation-token")`)
const replacement = await evaluate(
  `return document.querySelector("#issued-invitation-token").value`
)
if (replacement === oldRevokedToken)
  throw new Error("resend did not rotate the credential")
await clickText("完成")
await waitFor(`!document.querySelector("#issued-invitation-token")`)

await evaluate(`
  const row = ${rowExpression(revokedEmail)};
  const button = [...row.querySelectorAll("button")].find((item) => item.title === "撤销邀请");
  if (!(button instanceof HTMLButtonElement)) throw new Error("revoke button not found");
  button.click();
`)
await waitFor(`${rowExpression(revokedEmail)}?.textContent.includes("已撤销")`)

const layouts = [
  await capture("admin-invitations-desktop.png", 1440, 1000),
  await capture("admin-invitations-mobile.png", 390, 844),
]

await command("Network.clearBrowserCookies")
await navigate(`/accept-invitation?token=${encodeURIComponent(token)}`)
await waitFor(`document.querySelector("#invitation-login")`)
const publicLayout = await capture(
  "admin-accept-invitation-mobile.png",
  390,
  844
)
await setInput("#invitation-login", `invited-${suffix}`)
await setInput("#invitation-display-name", `Invited ${suffix}`)
await setInput("#invitation-password", "Chaosplus-Acceptance-2026!")
await setInput("#invitation-confirm-password", "Chaosplus-Acceptance-2026!")
await submit("form")
await waitFor(`document.body.innerText.includes("邀请已接受")`, 20000)

const invitationResponses = responses.filter((response) =>
  response.url.includes("/api/iam/invitations")
)
for (const expected of ["GET", "POST", "DELETE"])
  if (
    !invitationResponses.some(
      (response) => response.method === expected && response.status < 400
    )
  )
    throw new Error(`missing successful ${expected} invitation request`)

const unexpectedResponses = responses.filter(
  (response) =>
    response.status >= 400 &&
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
    credentialRotated: true,
    invitationRevoked: true,
    invitationAccepted: true,
    layouts,
    publicLayout,
    invitationRequests: invitationResponses.length,
    consoleErrors,
    unexpectedFailures,
    unexpectedResponses,
  })
)
