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
    const element = [...document.querySelectorAll("button")]
      .find((item) => item.textContent.trim() === ${JSON.stringify(text)} && !item.disabled);
    if (!(element instanceof HTMLButtonElement)) throw new Error("button not found: " + ${JSON.stringify(text)});
    element.click();
  `)
}

async function clickWithin(selector, text) {
  await evaluate(`
    const root = document.querySelector(${JSON.stringify(selector)});
    const button = [...(root?.querySelectorAll("button") ?? [])]
      .find((item) => item.textContent.trim() === ${JSON.stringify(text)} && !item.disabled);
    if (!(button instanceof HTMLButtonElement)) throw new Error("button not found in " + ${JSON.stringify(selector)});
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
  if (layout.horizontalOverflow || !layout.heading.includes("访问复核"))
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

async function decide(review, actorID) {
  const detail = await api(
    `/iam/access-reviews/${encodeURIComponent(review.id)}`
  )
  for (const item of detail.items.filter(
    (candidate) =>
      candidate.decision === "pending" && candidate.principal_id !== actorID
  )) {
    await clickWithin(`[data-review-item-id="${item.id}"]`, "保留")
    await waitFor(`document.querySelector("#access-review-action")`)
    await submit("#access-review-action")
    await waitFor(`!document.querySelector("#access-review-action")`)
  }
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
const campaignName = `Acceptance Review ${suffix}`
const reviewerLogin = `access-reviewer-${suffix}`
const reviewerEmail = `${reviewerLogin}@example.test`
const reviewerPassword = "Chaosplus-Access-Reviewer-2026!"

await login("admin@chaosplus.local", password)
const session = await api("/authn/session")
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
const invitation = await api("/iam/invitations", {
  method: "POST",
  body: JSON.stringify({
    email: reviewerEmail,
    role_ids: [administratorRole.id],
  }),
})
await logout()
const acceptance = await api("/iam/invitations/accept", {
  method: "POST",
  body: JSON.stringify({
    token: invitation.token,
    login_name: reviewerLogin,
    password: reviewerPassword,
    display_name: `Access Reviewer ${suffix}`,
  }),
})
await login("admin@chaosplus.local", password)

await navigate("/iam/access-reviews")
await waitFor(`document.querySelector("h1")?.textContent.includes("访问复核")`)
await clickText("新建复核")
await waitFor(`document.querySelector("#access-review-create")`)
await setControl("#access-review-name", campaignName)
await setControl(
  "#access-review-due-at",
  new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString().slice(0, 16)
)
await submit("#access-review-create")
await waitFor(
  `document.querySelector("#review-detail-heading")?.textContent.includes(${JSON.stringify(campaignName)})`,
  20000
)
const review = (await api("/iam/access-reviews")).find(
  (candidate) => candidate.name === campaignName
)
if (!review) throw new Error("created access review was not found")
await decide(review, session.subject)

await logout()
await login(reviewerLogin, reviewerPassword)
await navigate("/iam/access-reviews")
await waitFor(
  `document.querySelector(${JSON.stringify(`[data-review-id="${review.id}"]`)})`
)
await clickWithin(`[data-review-id="${review.id}"]`, "查看")
await waitFor(
  `document.querySelector("#review-detail-heading")?.textContent.includes(${JSON.stringify(campaignName)})`
)
await decide(review, acceptance.principal_id)
await waitFor(
  `[...document.querySelectorAll("button")].some((button) => button.textContent.trim() === "完成复核" && !button.disabled)`
)
await clickText("完成复核")
await waitFor(`document.querySelector("#access-review-action")`)
await submit("#access-review-action")
await waitFor(
  `document.querySelector("#review-detail-heading")?.parentElement?.textContent.includes("已完成")`
)

const layouts = [
  await capture("admin-access-reviews-desktop.png", 1440, 1000),
  await capture("admin-access-reviews-mobile.png", 390, 844),
]

await logout()
await login("admin@chaosplus.local", password)
await api(
  `/iam/roles/${encodeURIComponent(administratorRole.id)}/members/${encodeURIComponent(acceptance.principal_id)}`,
  { method: "DELETE" }
)

const governanceResponses = responses.filter((response) =>
  response.url.includes("/api/iam/access-reviews")
)
for (const expected of ["GET", "POST"])
  if (
    !governanceResponses.some(
      (response) => response.method === expected && response.status < 400
    )
  )
    throw new Error(`missing successful ${expected} access review`)

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
    reviewID: review.id,
    completed: true,
    layouts,
    governanceResponses,
  })
)
