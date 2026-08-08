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

async function clickText(text) {
  await evaluate(`
    const element = [...document.querySelectorAll("button")].find((item) => item.textContent.includes(${JSON.stringify(text)}));
    if (!(element instanceof HTMLButtonElement)) throw new Error("button not found");
    element.click();
  `)
}

async function submit(selector) {
  await evaluate(`
    const form = document.querySelector(${JSON.stringify(selector)});
    if (!(form instanceof HTMLFormElement)) throw new Error("form not found");
    form.requestSubmit();
  `)
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
    throw new Error(
      `Horizontal overflow at ${width}px: ${layout.scrollWidth}px`
    )
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
  command("WebAuthn.enable"),
])
await command("Network.clearBrowserCookies")
const authenticator = await command("WebAuthn.addVirtualAuthenticator", {
  options: {
    protocol: "ctap2",
    ctap2Version: "ctap2_1",
    transport: "internal",
    hasResidentKey: true,
    hasUserVerification: true,
    isUserVerified: true,
    automaticPresenceSimulation: true,
  },
})

await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})
await navigate("/login")
await waitFor(`document.querySelector("#login-name")`)
await setInput("#login-name", "admin@chaosplus.local")
await setInput("#password", password)
await submit("form")
await waitFor(`location.pathname === "/"`)

await navigate("/security")
await waitFor(
  `[...document.querySelectorAll("button")].some((item) => item.textContent.includes("添加通行密钥"))`
)
await clickText("添加通行密钥")
await setInput("#passkey-register-form-name", "Chrome platform key")
await setInput("#passkey-register-form-password", password)
await submit("#passkey-register-form")
await waitFor(`document.body.textContent.includes("通行密钥已添加")`, 20000)
await waitFor(`document.body.textContent.includes("Chrome platform key")`)

await evaluate(
  `document.querySelector('button[aria-label^="重命名通行密钥"]')?.click()`
)
await waitFor(`document.querySelector("#passkey-rename-form-name")`)
await setInput("#passkey-rename-form-name", "Primary passkey")
await submit("#passkey-rename-form")
await waitFor(`document.body.textContent.includes("通行密钥名称已更新")`)
await waitFor(`document.body.textContent.includes("Primary passkey")`)

const desktopLayout = await screenshot("admin-passkey-desktop.png", 1440, 1000)
const mobileLayout = await screenshot("admin-passkey-mobile.png", 390, 844)
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})

await evaluate(`
  await fetch("/api/authn/logout", { method: "POST", credentials: "include" });
  location.assign("/login");
`)
await waitFor(
  `location.pathname === "/login" && document.body.textContent.includes("使用通行密钥")`
)
await clickText("使用通行密钥")
await waitFor(`location.pathname === "/"`, 20000)
const authenticated = await evaluate(`
  const response = await fetch("/api/authn/session", { credentials: "include" });
  return response.ok;
`)
if (!authenticated)
  throw new Error("Passkey login did not create a browser session")

await navigate("/security")
await waitFor(`document.body.textContent.includes("Primary passkey")`)
await evaluate(
  `document.querySelector('button[aria-label^="删除通行密钥"]')?.click()`
)
await waitFor(`document.querySelector("#passkey-delete-form-password")`)
await setInput("#passkey-delete-form-password", password)
await submit("#passkey-delete-form")
await waitFor(`document.body.textContent.includes("通行密钥已删除")`)
await waitFor(`!document.body.textContent.includes("Primary passkey")`)

await evaluate(`
  await fetch("/api/authn/logout", { method: "POST", credentials: "include" });
  location.assign("/login");
`)
await waitFor(
  `location.pathname === "/login" && document.body.textContent.includes("使用通行密钥")`
)
await clickText("使用通行密钥")
await waitFor(`document.querySelector('[role="alert"]')`, 20000)
const rejectedAfterDeletion = await evaluate(
  `return location.pathname === "/login"`
)
if (!rejectedAfterDeletion)
  throw new Error("Deleted passkey unexpectedly authenticated")

const credentials = await command("WebAuthn.getCredentials", {
  authenticatorId: authenticator.authenticatorId,
})
await command("WebAuthn.removeVirtualAuthenticator", {
  authenticatorId: authenticator.authenticatorId,
})
socket.close()

console.log(
  JSON.stringify({
    registeredCredentials: credentials.credentials.length,
    authenticated,
    renamed: true,
    deleted: true,
    rejectedAfterDeletion,
    desktopLayout,
    mobileLayout,
    consoleErrors,
  })
)
