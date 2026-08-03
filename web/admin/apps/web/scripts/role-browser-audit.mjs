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
    if (!(input instanceof HTMLInputElement)) throw new Error("input not found: " + ${JSON.stringify(selector)});
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
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

async function apiRaw(path, options = {}) {
  return evaluate(`
    const response = await fetch(${JSON.stringify(`/api${path}`)}, {
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        "X-Tenant-Id": localStorage.getItem("chaosplus.tenant") || "platform",
      },
      ...${JSON.stringify(options)},
    });
    const payload = await response.json();
    return { status: response.status, payload };
  `)
}

async function api(path, options = {}) {
  const result = await apiRaw(path, options)
  if (result.status < 200 || result.status >= 300)
    throw new Error(
      `${path} failed: ${result.status} ${result.payload?.message ?? ""}`
    )
  return result.payload.data
}

async function cleanup() {
  for (const role of (await api("/iam/roles")).filter((item) =>
    item.name.startsWith("Acceptance Directory Role ")
  )) {
    for (const binding of await api(
      `/iam/roles/${encodeURIComponent(role.id)}/directory-bindings`
    ))
      await api(
        `/iam/roles/${encodeURIComponent(role.id)}/directory-bindings/${binding.assignee_type}/${encodeURIComponent(binding.assignee_id)}`,
        { method: "DELETE" }
      )
    for (const code of await api(
      `/iam/roles/${encodeURIComponent(role.id)}/permissions`
    ))
      await api(
        `/iam/roles/${encodeURIComponent(role.id)}/permissions/${encodeURIComponent(code)}`,
        { method: "DELETE" }
      )
    for (const subject of await api(
      `/iam/roles/${encodeURIComponent(role.id)}/members`
    ))
      await api(
        `/iam/roles/${encodeURIComponent(role.id)}/members/${encodeURIComponent(subject)}`,
        { method: "DELETE" }
      )
    await api(`/iam/roles/${encodeURIComponent(role.id)}`, {
      method: "DELETE",
    })
  }
  for (const group of (await api("/iam/groups")).filter((item) =>
    item.name.startsWith("Acceptance Role Group ")
  ))
    await api(
      `/iam/groups/${encodeURIComponent(group.id)}?version=${group.version}`,
      {
        method: "DELETE",
      }
    )
  for (const position of (await api("/iam/positions")).filter((item) =>
    item.code.startsWith("acceptance.role-")
  ))
    await api(
      `/iam/positions/${encodeURIComponent(position.id)}?version=${position.version}`,
      { method: "DELETE" }
    )
  for (const department of (await api("/iam/departments")).filter((item) =>
    item.name.startsWith("Acceptance Role Department ")
  ))
    await api(
      `/iam/departments/${encodeURIComponent(department.id)}?version=${department.version}`,
      { method: "DELETE" }
    )
}

async function capture(name, width, height, focusSelector = "") {
  await command("Emulation.setDeviceMetricsOverride", {
    width,
    height,
    deviceScaleFactor: 1,
    mobile: width < 600,
  })
  if (focusSelector)
    await evaluate(`
      const target = document.querySelector(${JSON.stringify(focusSelector)});
      if (!(target instanceof HTMLElement)) throw new Error("capture target not found");
      target.scrollIntoView({ block: "start" });
    `)
  await Bun.sleep(250)
  const layout = await evaluate(`return {
    width: innerWidth,
    scrollWidth: document.documentElement.scrollWidth,
    horizontalOverflow: document.documentElement.scrollWidth > innerWidth,
    groupSection: Boolean(document.querySelector('[data-directory-section="group"]')),
	positionSection: Boolean(document.querySelector('[data-directory-section="position"]')),
	dataScopeSection: Boolean(document.querySelector('[data-role-data-scope]')),
	permissionConditionSummary: Boolean(document.querySelector('[data-permission-condition-summary]')),
  }`)
  if (
    layout.horizontalOverflow ||
    !layout.groupSection ||
    !layout.positionSection ||
    !layout.dataScopeSection ||
    !layout.permissionConditionSummary
  )
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
await command("Network.clearBrowserCookies")
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})

await navigate("/iam/roles")
await waitFor(
  `location.pathname === "/login" && document.querySelector("#login-name")`
)
await setInput("#login-name", "admin@chaosplus.local")
await setInput("#password", password)
await evaluate(`document.querySelector("form").requestSubmit()`)
await waitFor(`location.pathname === "/iam/roles"`, 20000)
await waitFor(`document.querySelector("h1")`)
await cleanup()

const suffix = Date.now().toString(36)
const roleName = `Acceptance Directory Role ${suffix}`
const groupName = `Acceptance Role Group ${suffix}`
const positionCode = `acceptance.role-${suffix}`
const positionName = `Acceptance Role Position ${suffix}`
const departmentName = `Acceptance Role Department ${suffix}`
const group = await api("/iam/groups", {
  method: "POST",
  body: JSON.stringify({
    name: groupName,
    type: "static",
    description: "Role assignment acceptance",
    status: "active",
    sort_order: 10,
  }),
})
const position = await api("/iam/positions", {
  method: "POST",
  body: JSON.stringify({
    code: positionCode,
    name: positionName,
    status: "active",
    sort_order: 10,
  }),
})
const department = await api("/iam/departments", {
  method: "POST",
  body: JSON.stringify({
    name: departmentName,
    status: "active",
    sort_order: 10,
  }),
})

await click("header:has(h1) button")
await waitFor(`document.querySelector("#role-form")`)
await setInput("#role-name", roleName)
await setInput("#role-description", "Directory-derived permissions")
await click('button[form="role-form"][type="submit"]')
await waitFor(`!document.querySelector("#role-form")`)
await waitFor(
  `[...document.querySelectorAll("button strong")].some((item) => item.textContent.trim() === ${JSON.stringify(roleName)})`
)

const role = (await api("/iam/roles")).find((item) => item.name === roleName)
if (!role) throw new Error("created role was not returned")
await waitFor(
  `[...document.querySelectorAll("h2")].some((item) => item.textContent.trim() === ${JSON.stringify(roleName)})`
)

await click("#role-data-scope")
const selectedDepartmentsOption = `[...document.querySelectorAll('[role="option"]')].find((item) => item.textContent.trim() === "指定部门")`
await waitFor(selectedDepartmentsOption)
await evaluate(`${selectedDepartmentsOption}.click()`)
const departmentSelector = `[data-scope-department-id=${JSON.stringify(department.id)}]`
await waitFor(`document.querySelector(${JSON.stringify(departmentSelector)})`)
await click(departmentSelector)
await click("[data-save-data-scope]")
await waitFor(
  `document.querySelector(${JSON.stringify(departmentSelector)})?.getAttribute("aria-checked") === "true"`
)
const dataScope = await api(
  `/iam/roles/${encodeURIComponent(role.id)}/data-scope`
)
if (
  dataScope.scope !== "selected_departments" ||
  dataScope.department_ids.length !== 1 ||
  dataScope.department_ids[0] !== department.id
)
  throw new Error("role data scope did not persist")

const permissionSelector = `document.querySelector('[data-permission-code="menu_view"] [role="checkbox"]')`
await waitFor(permissionSelector)
await evaluate(`${permissionSelector}.click()`)
await waitFor(`${permissionSelector}?.getAttribute("aria-checked") === "true"`)
if (
  !(
    await api(`/iam/roles/${encodeURIComponent(role.id)}/permissions`)
  ).includes("menu_view")
)
  throw new Error("permission grant did not persist")

await click('[data-edit-permission-condition="menu_view"]')
await waitFor(`document.querySelector("#role-permission-condition-form")`)
await setInput("#permission-minimum-acr", "2")
await setInput("#permission-client-id", "admin-console")
await setInput("#permission-network-zone", "corporate")
await setInput("#permission-time-start", "08:00")
await setInput("#permission-time-end", "18:00")
await setInput("#permission-timezone", "Asia/Shanghai")
await click("[data-save-permission-condition]")
await waitFor(`!document.querySelector("#role-permission-condition-form")`)
const permissionGrant = (
  await api(`/iam/roles/${encodeURIComponent(role.id)}/permission-grants`)
).find((grant) => grant.permission_code === "menu_view")
if (
  !permissionGrant?.condition?.all ||
  permissionGrant.condition.all.length !== 4
)
  throw new Error("permission condition did not persist")
await waitFor(
  `document.querySelector('[data-permission-condition-summary]')?.textContent.includes("ACR >= 2")`
)

for (const [kind, item] of [
  ["group", group],
  ["position", position],
]) {
  const selector = `[data-assignee-type=${JSON.stringify(kind)}][data-assignee-id=${JSON.stringify(item.id)}]`
  await waitFor(`document.querySelector(${JSON.stringify(selector)})`)
  await click(selector)
  await waitFor(
    `document.querySelector(${JSON.stringify(selector)})?.getAttribute("aria-checked") === "true"`
  )
}

const bindings = await api(
  `/iam/roles/${encodeURIComponent(role.id)}/directory-bindings`
)
if (
  bindings.length !== 2 ||
  !bindings.some(
    (binding) =>
      binding.assignee_type === "group" && binding.assignee_id === group.id
  ) ||
  !bindings.some(
    (binding) =>
      binding.assignee_type === "position" &&
      binding.assignee_id === position.id
  )
)
  throw new Error("directory bindings did not persist")

const groupDelete = await apiRaw(
  `/iam/groups/${encodeURIComponent(group.id)}?version=${group.version}`,
  { method: "DELETE" }
)
const positionDelete = await apiRaw(
  `/iam/positions/${encodeURIComponent(position.id)}?version=${position.version}`,
  { method: "DELETE" }
)
if (groupDelete.status !== 409 || positionDelete.status !== 409)
  throw new Error("role-bound directory deletion was not protected")

const layouts = [
  await capture(
    "role-directory-desktop.png",
    1440,
    1000,
    '[data-permission-code="menu_view"]'
  ),
  await capture(
    "role-directory-mobile.png",
    390,
    844,
    '[data-permission-code="menu_view"]'
  ),
]
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})

for (const [kind, item] of [
  ["group", group],
  ["position", position],
]) {
  const selector = `[data-assignee-type=${JSON.stringify(kind)}][data-assignee-id=${JSON.stringify(item.id)}]`
  await click(selector)
  await waitFor(
    `document.querySelector(${JSON.stringify(selector)})?.getAttribute("aria-checked") === "false"`
  )
}

await click('[data-edit-permission-condition="menu_view"]')
await waitFor(`document.querySelector("[data-clear-permission-condition]")`)
await click("[data-clear-permission-condition]")
await waitFor(`!document.querySelector("#role-permission-condition-form")`)
const clearedPermissionGrant = (
  await api(`/iam/roles/${encodeURIComponent(role.id)}/permission-grants`)
).find((grant) => grant.permission_code === "menu_view")
if (!clearedPermissionGrant || clearedPermissionGrant.condition)
  throw new Error("permission condition did not clear")

const audit = await api("/iam/audit-events?limit=200")
for (const type of [
  "role_directory_binding_added",
  "role_directory_binding_removed",
]) {
  const count = audit.filter(
    (event) => event.event_type === type && event.target_id === role.id
  ).length
  if (count !== 2)
    throw new Error(`expected two ${type} audit events, got ${count}`)
}
const scopeAuditCount = audit.filter(
  (event) =>
    event.event_type === "role_data_scope_updated" &&
    event.target_id === role.id
).length
if (scopeAuditCount !== 1)
  throw new Error(
    `expected one role_data_scope_updated audit event, got ${scopeAuditCount}`
  )
const conditionAuditCount = audit.filter(
  (event) =>
    event.event_type === "role_permission_condition_updated" &&
    event.target_id === role.id
).length
if (conditionAuditCount !== 2)
  throw new Error(
    `expected two role_permission_condition_updated audit events, got ${conditionAuditCount}`
  )

await cleanup()
const unexpectedResponses = responses.filter(
  (response) =>
    response.status >= 400 && response.status !== 401 && response.status !== 409
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
      roleID: role.id,
      groupID: group.id,
      positionID: position.id,
      departmentID: department.id,
      layouts,
      auditEvents: 7,
      consoleErrors,
      failedRequests: unexpectedFailures,
      unexpectedResponses,
    },
    null,
    2
  )
)
