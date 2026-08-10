const port = Number(process.argv[2] ?? 9333)
const baseURL = process.argv[3] ?? "http://localhost:8091"
const password = process.env.CHAOSPLUS_BROWSER_PASSWORD
const directoryKind = process.env.CHAOSPLUS_DIRECTORY_KIND ?? "position"
const directory = {
  position: {
    collectionPath: "/iam/positions",
    formID: "position-form",
    memberFormID: "position-member-form",
    memberInputPrefix: "position-member",
    scheduleTitle: "任职时间",
    screenshotName: "positions",
  },
  group: {
    collectionPath: "/iam/groups",
    formID: "group-form",
    memberFormID: "group-member-form",
    memberInputPrefix: "group-member",
    scheduleTitle: "有效时间",
    screenshotName: "groups",
  },
}[directoryKind]
const screenshotRoot = new URL(
  "../../../../../.local/screenshots/",
  import.meta.url
)

if (!password) throw new Error("CHAOSPLUS_BROWSER_PASSWORD is required")
if (!directory) throw new Error(`unsupported directory kind: ${directoryKind}`)

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
    if (!(input instanceof HTMLInputElement) && !(input instanceof HTMLTextAreaElement)) throw new Error("input not found: " + ${JSON.stringify(selector)});
    const prototype = input instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(prototype, "value").set;
    setter.call(input, ${JSON.stringify(value)});
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  `)
}

async function click(selector) {
  await evaluate(`
    const button = document.querySelector(${JSON.stringify(selector)});
    if (!(button instanceof HTMLButtonElement)) throw new Error("button not found: " + ${JSON.stringify(selector)});
    button.click();
  `)
}

async function chooseOption(triggerSelector, label) {
  await click(triggerSelector)
  await waitFor(`document.querySelector('[role="option"]')`)
  const point = await evaluate(`
    const option = [...document.querySelectorAll('[role="option"]')].find(
      (item) => item.textContent.includes(${JSON.stringify(label)})
    );
    if (!(option instanceof HTMLElement)) throw new Error("select option not found");
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
  await waitFor(
    `document.querySelector(${JSON.stringify(triggerSelector)})?.getAttribute("aria-expanded") !== "true"`
  )
}

function itemRow(name) {
  return `[...document.querySelectorAll("tbody tr")].find((row) => [...row.querySelectorAll("strong")].some((item) => item.textContent.trim() === ${JSON.stringify(name)}))`
}

function memberRow(principalID) {
  return `[...document.querySelectorAll("tbody tr")].find((row) => row.textContent.includes(${JSON.stringify(principalID)}))`
}

async function openDirectoryForm(actionSelector) {
  await click(actionSelector)
  await waitFor(
    `document.querySelector(${JSON.stringify(`#${directory.formID}`)})`
  )
}

async function submitDirectoryForm(expectClose = true) {
  await click(`button[form="${directory.formID}"][type="submit"]`)
  if (expectClose)
    await waitFor(
      `!document.querySelector(${JSON.stringify(`#${directory.formID}`)})`
    )
}

async function submitMemberForm() {
  await click(`button[form="${directory.memberFormID}"][type="submit"]`)
  await waitFor(
    `!document.querySelector(${JSON.stringify(`button[form="${directory.memberFormID}"][type="submit"]`)})?.disabled`
  )
}

async function pressEscape() {
  await command("Input.dispatchKeyEvent", {
    type: "keyDown",
    key: "Escape",
    code: "Escape",
  })
  await command("Input.dispatchKeyEvent", {
    type: "keyUp",
    key: "Escape",
    code: "Escape",
  })
}

async function api(path, options = {}) {
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
    if (!response.ok) throw new Error(${JSON.stringify(path)} + " failed: " + response.status + " " + payload.message);
    return payload.data;
  `)
}

async function cleanupArtifacts() {
  const items = await api(directory.collectionPath)
  const artifacts = items.filter((item) =>
    directoryKind === "position"
      ? item.code.startsWith("acceptance.")
      : item.name.startsWith("Acceptance Group ") ||
        item.name.startsWith("Acceptance Security Group ") ||
        item.name.startsWith("Acceptance Dynamic Group ")
  )
  for (const item of artifacts) {
    if (directoryKind !== "group" || item.type === "static") {
      const members = await api(
        `${directory.collectionPath}/${encodeURIComponent(item.id)}/members`
      )
      for (const member of members)
        await api(
          `${directory.collectionPath}/${encodeURIComponent(item.id)}/members/${encodeURIComponent(member.principal_id)}`,
          { method: "DELETE" }
        )
    }
    await api(
      `${directory.collectionPath}/${encodeURIComponent(item.id)}?version=${item.version}`,
      { method: "DELETE" }
    )
  }
  await command("Page.reload", { ignoreCache: true })
  await waitFor(`document.readyState === "complete"`)
  await waitFor(
    `document.querySelector("h1") && document.querySelector("tbody")`
  )
}

async function capture(name, width, height, expectDialog = false) {
  await command("Emulation.setDeviceMetricsOverride", {
    width,
    height,
    deviceScaleFactor: 1,
    mobile: width < 600,
  })
  await Bun.sleep(250)
  const layout = await evaluate(`
    const dialog = document.querySelector('[role="dialog"]');
    const rect = dialog?.getBoundingClientRect();
    return {
      width: innerWidth,
      scrollWidth: document.documentElement.scrollWidth,
      horizontalOverflow: document.documentElement.scrollWidth > innerWidth,
      dialogVisible: Boolean(dialog),
      dialogWithinViewport: !rect || (rect.left >= 0 && rect.right <= innerWidth && rect.top >= 0 && rect.bottom <= innerHeight),
    };
  `)
  if (layout.horizontalOverflow)
    throw new Error(
      `horizontal overflow at ${width}px: ${layout.scrollWidth}px`
    )
  if (expectDialog && (!layout.dialogVisible || !layout.dialogWithinViewport))
    throw new Error(
      `member dialog is clipped at ${width}px: ${JSON.stringify(layout)}`
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
])
await command("Network.clearBrowserCookies")
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})

const suffix = Date.now().toString(36)
const code = `acceptance.platform-${suffix}`
const originalName =
  directoryKind === "position"
    ? `Acceptance Platform ${suffix}`
    : `Acceptance Group ${suffix}`
const updatedName =
  directoryKind === "position"
    ? `Acceptance Principal Platform ${suffix}`
    : `Acceptance Security Group ${suffix}`

await navigate(directory.collectionPath)
await waitFor(
  `location.pathname === "/login" && document.querySelector("#login-name")`
)
await setInput("#login-name", "admin@chaosplus.local")
await setInput("#password", password)
await evaluate(`
  const form = document.querySelector("form");
  if (!(form instanceof HTMLFormElement)) throw new Error("login form not found");
  form.requestSubmit();
`)
await waitFor(
  `location.pathname === ${JSON.stringify(directory.collectionPath)}`,
  20000
)
await waitFor(`document.querySelector("h1") && document.querySelector("tbody")`)
await cleanupArtifacts()

await openDirectoryForm("header:has(h1) button")
if (directoryKind === "position") {
  await setInput("#position-code", code.toUpperCase())
  await setInput("#position-name", originalName)
  await setInput("#position-sort-order", "10")
} else {
  await setInput("#group-name", originalName)
  await setInput("#group-description", "Acceptance directory group")
  await setInput("#group-sort-order", "10")
}
await submitDirectoryForm()
await waitFor(itemRow(originalName))

let items = await api(directory.collectionPath)
const created = items.find((item) =>
  directoryKind === "position" ? item.code === code : item.name === originalName
)
if (
  !created ||
  created.name !== originalName ||
  created.version !== 1 ||
  (directoryKind === "group" && created.type !== "static")
)
  throw new Error(`created ${directoryKind} state is incorrect`)

await openDirectoryForm("header:has(h1) button")
if (directoryKind === "position") {
  await setInput("#position-code", code)
  await setInput("#position-name", `${originalName} Duplicate`)
} else {
  await setInput("#group-name", originalName.toUpperCase())
  await setInput("#group-description", "Duplicate")
}
await submitDirectoryForm(false)
await waitFor(`document.querySelector('[role="alert"]')`)
await pressEscape()
await waitFor(
  `!document.querySelector(${JSON.stringify(`#${directory.formID}`)})`
)

await openDirectoryForm(
  `button[aria-label=${JSON.stringify(`编辑${originalName}`)}]`
)
if (directoryKind === "position") {
  await setInput("#position-name", updatedName)
  await setInput("#position-sort-order", "11")
} else {
  await setInput("#group-name", updatedName)
  await setInput("#group-description", "Privileged security operators")
  await setInput("#group-sort-order", "11")
}
await submitDirectoryForm()
await waitFor(itemRow(updatedName))

items = await api(directory.collectionPath)
const updated = items.find((item) => item.id === created.id)
if (
  !updated ||
  updated.name !== updatedName ||
  updated.sort_order !== 11 ||
  updated.version !== 2 ||
  (directoryKind === "group" &&
    updated.description !== "Privileged security operators")
)
  throw new Error(`updated ${directoryKind} state is incorrect`)

const tenantMembers = await api("/iam/members")
const assignedBefore = await api(
  `${directory.collectionPath}/${encodeURIComponent(created.id)}/members`
)
const principal = tenantMembers.find(
  (item) =>
    item.status === "active" &&
    !assignedBefore.some((member) => member.principal_id === item.subject)
)
if (!principal) throw new Error("no active tenant member is available")
const principalLabel = principal.display_name || principal.subject

await click(`button[aria-label=${JSON.stringify(`管理${updatedName}成员`)}]`)
await waitFor(
  `document.querySelector(${JSON.stringify(`#${directory.memberFormID}`)})`
)
await chooseOption(`#${directory.memberInputPrefix}`, principalLabel)
const start = new Date(Date.now() + 24 * 60 * 60 * 1000)
const end = new Date(Date.now() + 48 * 60 * 60 * 1000)
const localValue = (value) => {
  const offset = value.getTimezoneOffset() * 60_000
  return new Date(value.getTime() - offset).toISOString().slice(0, 16)
}
await setInput(`#${directory.memberInputPrefix}-start`, localValue(start))
await setInput(`#${directory.memberInputPrefix}-end`, localValue(end))
await submitMemberForm()
await waitFor(memberRow(principal.subject))

let assigned = await api(
  `${directory.collectionPath}/${encodeURIComponent(created.id)}/members`
)
let assignment = assigned.find(
  (item) => item.principal_id === principal.subject
)
if (
  !assignment?.starts_at ||
  !assignment.ends_at ||
  Date.parse(assignment.ends_at) <= Date.parse(assignment.starts_at)
)
  throw new Error(`scheduled ${directoryKind} assignment is incorrect`)

const memberDialogDesktop = await capture(
  `admin-${directory.screenshotName}-members-desktop.png`,
  1440,
  1000,
  true
)
const memberDialogMobile = await capture(
  `admin-${directory.screenshotName}-members-mobile.png`,
  390,
  844,
  true
)
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})
await pressEscape()
await waitFor(
  `!document.querySelector(${JSON.stringify(`#${directory.memberFormID}`)})`
)

await click(`button[aria-label=${JSON.stringify(`删除${updatedName}`)}]`)
await waitFor(`document.querySelector('[role="alert"]')`)
if (!(await evaluate(`return Boolean(${itemRow(updatedName)})`)))
  throw new Error(`${directoryKind} with members was deleted`)

await click(`button[aria-label=${JSON.stringify(`管理${updatedName}成员`)}]`)
await waitFor(memberRow(principal.subject))
await evaluate(`
  const row = ${memberRow(principal.subject)};
  const button = row?.querySelector(${JSON.stringify(`button[title="编辑${directory.scheduleTitle}"]`)});
  if (!(button instanceof HTMLButtonElement)) throw new Error("member edit button not found");
  button.click();
`)
await setInput(`#${directory.memberInputPrefix}-start`, "")
await setInput(`#${directory.memberInputPrefix}-end`, "")
await submitMemberForm()
await waitFor(
  `!document.querySelector(${JSON.stringify(`button[form="${directory.memberFormID}"][type="submit"]`)})?.textContent.includes("更新")`
)

assigned = await api(
  `${directory.collectionPath}/${encodeURIComponent(created.id)}/members`
)
assignment = assigned.find((item) => item.principal_id === principal.subject)
if (!assignment || assignment.starts_at || assignment.ends_at)
  throw new Error(`open-ended ${directoryKind} assignment is incorrect`)

await evaluate(`
  const row = ${memberRow(principal.subject)};
  const button = row?.querySelector('button[title="移除成员"]');
  if (!(button instanceof HTMLButtonElement)) throw new Error("member remove button not found");
  button.click();
`)
await waitFor(`!(${memberRow(principal.subject)})`)
await pressEscape()
await waitFor(
  `!document.querySelector(${JSON.stringify(`#${directory.memberFormID}`)})`
)

const pageDesktop = await capture(
  `admin-${directory.screenshotName}-desktop.png`,
  1440,
  1000
)
const pageMobile = await capture(
  `admin-${directory.screenshotName}-mobile.png`,
  390,
  844
)
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})
await click(`button[aria-label=${JSON.stringify(`删除${updatedName}`)}]`)
await waitFor(`!(${itemRow(updatedName)})`)

const audit = await api(
  `/iam/audit-events?target_id=${encodeURIComponent(created.id)}&limit=100`
)
const eventTypes = new Set(audit.map((event) => event.event_type))
for (const eventType of [
  `${directoryKind}_created`,
  `${directoryKind}_updated`,
  `${directoryKind}_member_assigned`,
  `${directoryKind}_member_removed`,
  `${directoryKind}_deleted`,
])
  if (!eventTypes.has(eventType))
    throw new Error(`missing audit event: ${eventType}`)

let dynamicGroup
let dynamicRuleDialogDesktop
let dynamicRuleDialogMobile
let dynamicMembersDesktop
let dynamicMembersMobile
if (directoryKind === "group") {
  const dynamicName = `Acceptance Dynamic Group ${suffix}`
  await openDirectoryForm("header:has(h1) button")
  await setInput("#group-name", dynamicName)
  await setInput("#group-description", "Computed acceptance directory group")
  await chooseOption("#group-type", "动态用户组")
  await chooseOption("#group-rule-field-0", "主体 ID")
  await setInput('input[aria-label="条件 1 值"]', principal.subject)
  dynamicRuleDialogDesktop = await capture(
    "admin-groups-dynamic-rule-desktop.png",
    1440,
    1000,
    true
  )
  dynamicRuleDialogMobile = await capture(
    "admin-groups-dynamic-rule-mobile.png",
    390,
    844,
    true
  )
  await command("Emulation.setDeviceMetricsOverride", {
    width: 1440,
    height: 1000,
    deviceScaleFactor: 1,
    mobile: false,
  })
  await submitDirectoryForm()
  await waitFor(itemRow(dynamicName))
  items = await api(directory.collectionPath)
  dynamicGroup = items.find((item) => item.name === dynamicName)
  if (
    dynamicGroup?.type !== "dynamic" ||
    dynamicGroup.membership_rule?.conditions?.[0]?.values?.[0] !==
      principal.subject
  )
    throw new Error("dynamic group rule was not persisted")

  await click(`button[aria-label=${JSON.stringify(`查看${dynamicName}成员`)}]`)
  await waitFor(memberRow(principal.subject))
  if (
    await evaluate(
      `return Boolean(document.querySelector('button[form="group-member-form"]'))`
    )
  )
    throw new Error("dynamic group member dialog exposed a mutation action")
  dynamicMembersDesktop = await capture(
    "admin-groups-dynamic-members-desktop.png",
    1440,
    1000,
    true
  )
  dynamicMembersMobile = await capture(
    "admin-groups-dynamic-members-mobile.png",
    390,
    844,
    true
  )
  await command("Emulation.setDeviceMetricsOverride", {
    width: 1440,
    height: 1000,
    deviceScaleFactor: 1,
    mobile: false,
  })
  await pressEscape()
  await waitFor(`!document.querySelector('[role="dialog"]')`)

  await openDirectoryForm(
    `button[aria-label=${JSON.stringify(`编辑${dynamicName}`)}]`
  )
  await setInput('input[aria-label="条件 1 值"]', `missing-${suffix}`)
  await submitDirectoryForm()
  await waitFor(itemRow(dynamicName))
  items = await api(directory.collectionPath)
  dynamicGroup = items.find((item) => item.id === dynamicGroup.id)
  if (
    dynamicGroup.version !== 2 ||
    dynamicGroup.membership_rule.conditions[0].values[0] !== `missing-${suffix}`
  )
    throw new Error("dynamic group rule update was not persisted")
  await click(`button[aria-label=${JSON.stringify(`查看${dynamicName}成员`)}]`)
  await waitFor(
    `document.querySelector('[role="dialog"]')?.textContent.includes("暂无用户组成员")`
  )
  await pressEscape()
  await click(`button[aria-label=${JSON.stringify(`删除${dynamicName}`)}]`)
  await waitFor(`!(${itemRow(dynamicName)})`)

  const dynamicAudit = await api(
    `/iam/audit-events?target_id=${encodeURIComponent(dynamicGroup.id)}&limit=100`
  )
  const dynamicEvents = new Set(dynamicAudit.map((event) => event.event_type))
  for (const eventType of ["group_created", "group_updated", "group_deleted"])
    if (!dynamicEvents.has(eventType))
      throw new Error(`missing dynamic group audit event: ${eventType}`)
}

const expectedConflicts = responses.filter(
  (response) =>
    response.status === 409 &&
    response.url.includes(`/api${directory.collectionPath}`)
)
if (expectedConflicts.length !== 2)
  throw new Error(
    `expected duplicate and member-protection conflicts: ${JSON.stringify(expectedConflicts)}`
  )
const unexpectedStatuses = responses.filter(
  (response) =>
    response.status >= 400 &&
    !(response.status === 401 && response.url.endsWith("/api/authn/session")) &&
    !expectedConflicts.includes(response)
)
const unexpectedFailures = failedRequests.filter(
  (request) => !request.canceled && request.error !== "net::ERR_ABORTED"
)
socket.close()

if (consoleErrors.length)
  throw new Error(`console errors: ${JSON.stringify(consoleErrors)}`)
if (unexpectedFailures.length)
  throw new Error(`failed requests: ${JSON.stringify(unexpectedFailures)}`)
if (unexpectedStatuses.length)
  throw new Error(
    `unexpected HTTP statuses: ${JSON.stringify(unexpectedStatuses)}`
  )

console.log(
  JSON.stringify({
    anonymousDeepLinkReturned: true,
    createConflictUpdate: true,
    scheduledAndOpenEndedAssignment: true,
    deletionProtection: true,
    cleanupCompleted: true,
    auditEvents: [...eventTypes].sort(),
    dynamicGroupCompleted: directoryKind === "group",
    dynamicRuleDialogDesktop,
    dynamicRuleDialogMobile,
    dynamicMembersDesktop,
    dynamicMembersMobile,
    pageDesktop,
    pageMobile,
    memberDialogDesktop,
    memberDialogMobile,
    consoleErrors,
    failedRequests: unexpectedFailures,
    unexpectedStatuses,
  })
)
