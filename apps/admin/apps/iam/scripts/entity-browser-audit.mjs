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
  const target = new URL(path, baseURL).href
  await command("Page.navigate", { url: target })
  await waitFor(
    `location.href === ${JSON.stringify(target)} && document.readyState === "complete"`
  )
}

async function setInput(selector, value) {
  await evaluate(`
    const input = document.querySelector(${JSON.stringify(selector)});
    if (!(input instanceof HTMLInputElement || input instanceof HTMLTextAreaElement))
      throw new Error("input not found: " + ${JSON.stringify(selector)});
    const setter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input), "value").set;
    setter.call(input, ${JSON.stringify(value)});
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  `)
}

async function selectOption(triggerSelector, label) {
  await evaluate(`
    const trigger = document.querySelector(${JSON.stringify(triggerSelector)});
    if (!(trigger instanceof HTMLButtonElement)) throw new Error("select trigger not found");
		trigger.scrollIntoView({ block: "center" });
		await new Promise((resolve) => setTimeout(resolve, 100));
    trigger.click();
  `)
  await waitFor(`document.querySelector('[role="option"]')`)
  await evaluate(`
    const option = [...document.querySelectorAll('[role="option"]')].find(
      (item) => item.textContent.includes(${JSON.stringify(label)})
    );
    if (!(option instanceof HTMLElement)) throw new Error("select option not found");
    option.click();
  `)
  await waitFor(
    `document.querySelector(${JSON.stringify(triggerSelector)})?.textContent.includes(${JSON.stringify(label)})`
  )
}

function rowExpression(name) {
  return `[...document.querySelectorAll("tbody tr")].find((row) => [...row.querySelectorAll("strong")].some((item) => item.textContent.trim() === ${JSON.stringify(name)}))`
}

async function submit(formID) {
  await evaluate(`
    const form = document.querySelector(${JSON.stringify(`#${formID}`)});
    if (!(form instanceof HTMLFormElement)) throw new Error("form not found");
    form.requestSubmit();
  `)
}

async function openCreateDialog() {
  await evaluate(`
    const button = [...document.querySelectorAll("button")].find(
      (item) => item.textContent.trim() === "创建实体"
    );
    if (!(button instanceof HTMLButtonElement)) throw new Error("create button not found");
    button.click();
  `)
  await waitFor(`document.querySelector("#entity-form")`)
}

async function createEntity(name, type, metadata, parent = "") {
  await openCreateDialog()
  await setInput("#entity-name", name)
  await setInput("#entity-type", type)
  await setInput("#entity-metadata", JSON.stringify(metadata))
  if (parent) await selectOption("#entity-parent", parent)
  await submit("entity-form")
  await waitFor(`!document.querySelector("#entity-form")`)
  await waitFor(rowExpression(name))
}

async function clickRowAction(name, title) {
  await evaluate(`
    const row = ${rowExpression(name)};
    if (!(row instanceof HTMLTableRowElement)) throw new Error("entity row not found");
    const button = [...row.querySelectorAll("button")].find((item) => item.title === ${JSON.stringify(title)});
    if (!(button instanceof HTMLButtonElement)) throw new Error("row action not found: " + ${JSON.stringify(title)});
    button.click();
  `)
}

async function entities() {
  return evaluate(`
    const response = await fetch("/api/iam/entities", {
      credentials: "include",
      headers: { "X-Tenant-Id": localStorage.getItem("chaosplus.tenant") || "platform" },
    });
    if (!response.ok) throw new Error("entity list failed: " + response.status);
    return (await response.json()).data;
  `)
}

async function cleanupArtifacts() {
  await evaluate(`
    const tenant = localStorage.getItem("chaosplus.tenant") || "platform";
    const headers = { "X-Tenant-Id": tenant };
    const list = await fetch("/api/iam/entities", { credentials: "include", headers });
    if (!list.ok) throw new Error("artifact list failed: " + list.status);
    const all = (await list.json()).data;
    const artifacts = all.filter((item) => /^Acceptance-(Company|Store)-[a-z0-9]+/.test(item.name));
    const artifactIDs = new Set(artifacts.map((item) => item.id));
    const relationshipList = await fetch("/api/iam/relationships", { credentials: "include", headers });
    if (relationshipList.ok) {
      for (const relationship of (await relationshipList.json()).data) {
        if (!artifactIDs.has(relationship.entity_id) && !artifactIDs.has(relationship.resource_id) && !(relationship.subject_type === "entity" && artifactIDs.has(relationship.subject_id))) continue;
        const query = new URLSearchParams();
        for (const key of ["entity_id", "subject_type", "subject_id", "subject_relation", "relation", "resource_type", "resource_id"])
          if (relationship[key]) query.set(key, relationship[key]);
        const removed = await fetch("/api/iam/relationships?" + query, { method: "DELETE", credentials: "include", headers });
        if (!removed.ok) throw new Error("relationship cleanup failed: " + removed.status);
      }
    }
    const byID = new Map(all.map((item) => [item.id, item]));
    const depth = (item) => {
      let result = 0;
      for (let parent = item.parent_id; parent; parent = byID.get(parent)?.parent_id) result++;
      return result;
    };
    artifacts.sort((left, right) => depth(right) - depth(left));
    for (const item of artifacts) {
      const listed = await fetch("/api/iam/entities/" + encodeURIComponent(item.id) + "/role-bindings", { credentials: "include", headers });
      if (listed.ok) {
        for (const binding of (await listed.json()).data) {
          await fetch(
            "/api/iam/entities/" + encodeURIComponent(item.id) + "/role-bindings/" + encodeURIComponent(binding.role_id) + "/" + encodeURIComponent(binding.principal_id),
            { method: "DELETE", credentials: "include", headers }
          );
        }
      }
      const removed = await fetch("/api/iam/entities/" + encodeURIComponent(item.id), { method: "DELETE", credentials: "include", headers });
      if (!removed.ok && removed.status !== 404)
        throw new Error("artifact cleanup failed: " + removed.status);
    }
  `)
  await navigate(`/iam/entities?audit=${Date.now()}`)
  await waitFor(
    `document.querySelector("tbody") && [...document.querySelectorAll("button")].some((item) => item.textContent.trim() === "创建实体")`
  )
}

async function screenshot(name, width, height) {
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
])
await command("Network.clearBrowserCookies")
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})

const suffix = Date.now().toString(36)
const rootName = `Acceptance-Company-${suffix}`
const childName = `Acceptance-Store-${suffix}`
const updatedName = `${childName}-Updated`
const businessResourceID = `acceptance-store-resource-${suffix}`

await navigate("/iam/entities")
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
await waitFor(`location.pathname === "/iam/entities"`, 20000)
await waitFor(`document.querySelector("h1") && document.querySelector("tbody")`)
await cleanupArtifacts()

const entityPostsBeforeInvalidMetadata = responses.filter(
  (response) =>
    response.method === "POST" && response.url.endsWith("/api/iam/entities")
).length
await openCreateDialog()
await setInput("#entity-name", `Invalid-Metadata-${suffix}`)
await setInput("#entity-type", "company")
await setInput("#entity-metadata", "[]")
await submit("entity-form")
await waitFor(
  `document.querySelector('[role="dialog"] [role="alert"]')?.textContent.includes("元数据必须是 JSON 对象")`
)
const entityPostsAfterInvalidMetadata = responses.filter(
  (response) =>
    response.method === "POST" && response.url.endsWith("/api/iam/entities")
).length
if (entityPostsAfterInvalidMetadata !== entityPostsBeforeInvalidMetadata)
  throw new Error("invalid metadata reached the API")
await evaluate(`
  const button = [...document.querySelectorAll('[role="dialog"] button')].find((item) => item.textContent.trim() === "取消");
  if (!(button instanceof HTMLButtonElement)) throw new Error("entity cancel button not found");
  button.click();
`)
await waitFor(`!document.querySelector("#entity-form")`)

await createEntity(rootName, "company", { region: "west" })
await createEntity(childName, "store", { tier: 1 }, rootName)

let state = await entities()
const root = state.find((item) => item.name === rootName)
const child = state.find((item) => item.name === childName)
if (!root || !child || child.parent_id !== root.id)
  throw new Error("created entity hierarchy is incorrect")

await evaluate(`
  const row = ${rowExpression(rootName)};
  const button = row?.querySelector("button");
  if (!(button instanceof HTMLButtonElement)) throw new Error("collapse button not found");
  button.click();
`)
await waitFor(`!(${rowExpression(childName)})`)
await evaluate(`
  const row = ${rowExpression(rootName)};
  const button = row?.querySelector("button");
  button.click();
`)
await waitFor(rowExpression(childName))

await clickRowAction(rootName, "删除空实体")
await waitFor(`document.querySelector('[role="alert"]')`)

await clickRowAction(childName, "编辑实体")
await waitFor(`document.querySelector("#entity-form")`)
await setInput("#entity-name", updatedName)
await setInput(
  "#entity-metadata",
  JSON.stringify({ tier: 2, code: "flagship" })
)
await submit("entity-form")
await waitFor(`!document.querySelector("#entity-form")`)
await waitFor(rowExpression(updatedName))

await clickRowAction(rootName, "角色绑定")
await waitFor(`document.querySelector("#entity-relationship-form")`)
await selectOption("#relationship-relation", "所有者")
await setInput(
  "#relationship-ends-at",
  new Date(Date.now() + 2 * 86400000).toISOString().slice(0, 16)
)
await setInput("#relationship-minimum-acr", "1")
await submit("entity-relationship-form")
await waitFor(
  `[...document.querySelectorAll('[role="dialog"] button')].some((item) => item.title === "撤销关系")`
)
await waitFor(
  `document.querySelector('[data-relationship-window]') && !document.querySelector('[data-relationship-window]')?.textContent.includes("长期有效")`
)
await waitFor(
  `document.querySelector('[data-relationship-condition]')?.textContent.includes("ACR >= 1")`
)
await evaluate(`
  const close = [...document.querySelectorAll('[role="dialog"] button')].find((button) => button.textContent.trim() === "关闭");
  if (!(close instanceof HTMLButtonElement)) throw new Error("relationship close button not found");
  close.click();
`)
await waitFor(`!document.querySelector("#entity-relationship-form")`)

await clickRowAction(updatedName, "角色绑定")
await waitFor(`document.querySelector("#entity-binding-form")`)
await selectOption("#relationship-subject-type", "实体")
await selectOption("#relationship-subject", rootName)
await submit("entity-relationship-form")
await waitFor(
  `document.querySelector('[role="dialog"]')?.textContent.includes("viewer") && [...document.querySelectorAll('[role="dialog"] button')].some((item) => item.title === "撤销关系")`
)
await setInput("#relationship-resource-id", businessResourceID)
await selectOption("#relationship-resource-type", "store")
await submit("entity-relationship-form")
await waitFor(
  `document.querySelector('[role="dialog"]')?.textContent.includes(${JSON.stringify(businessResourceID)})`
)
const businessResourceCheck = await evaluate(`
  const headers = { "X-Tenant-Id": localStorage.getItem("chaosplus.tenant") || "platform" };
  const members = await fetch("/api/iam/members", { credentials: "include", headers });
  if (!members.ok) throw new Error("member list failed: " + members.status);
  const principal = (await members.json()).data.find((item) => item.status === "active")?.subject;
  const response = await fetch("/api/iam/authorization/check", {
    method: "POST",
    credentials: "include",
    headers: { ...headers, "Content-Type": "application/json" },
    body: JSON.stringify({
      entity_id: ${JSON.stringify(child.id)},
      resource_type: "store",
      resource_id: ${JSON.stringify(businessResourceID)},
      permission_code: "store_view",
      subject: principal,
    }),
  });
  return { status: response.status, body: await response.json() };
`)
if (
  businessResourceCheck.status !== 200 ||
  !businessResourceCheck.body.data.allowed
)
  throw new Error(
    `business resource check failed: ${JSON.stringify(businessResourceCheck)}`
  )
const localizedWindowErrors = await evaluate(`
  const baseHeaders = { "X-Tenant-Id": localStorage.getItem("chaosplus.tenant") || "platform" };
  const members = await fetch("/api/iam/members", { credentials: "include", headers: baseHeaders });
  if (!members.ok) throw new Error("member list failed: " + members.status);
  const principal = (await members.json()).data.find((item) => item.status === "active")?.subject;
  const results = [];
  for (const locale of ["en-US", "zh-CN", "ms-MY"]) {
    const response = await fetch("/api/iam/relationships", {
      method: "POST",
      credentials: "include",
      headers: { ...baseHeaders, "X-Lang": locale, "Content-Type": "application/json" },
      body: JSON.stringify({
        subject_type: "principal",
        subject_id: principal,
        relation: "editor",
        resource_type: "store",
        resource_id: ${JSON.stringify(child.id)},
        ends_at: new Date(Date.now() - 60000).toISOString(),
      }),
    });
    results.push({ locale, status: response.status, body: await response.json() });
  }
  return results;
`)
if (
  localizedWindowErrors.some(
    (result) =>
      result.status !== 422 ||
      !result.body.message ||
      result.body.message === "invalid_relationship_window"
  ) ||
  new Set(localizedWindowErrors.map((result) => result.body.message)).size !== 3
)
  throw new Error(
    `relationship window localization failed: ${JSON.stringify(localizedWindowErrors)}`
  )
const expectedWindowStatuses = responses.filter(
  (response) =>
    response.status === 422 &&
    response.method === "POST" &&
    response.url.endsWith("/api/iam/relationships")
)
if (expectedWindowStatuses.length !== 3)
  throw new Error(
    `expected three localized window failures, got ${expectedWindowStatuses.length}`
  )
const localizedConditionErrors = await evaluate(`
  const baseHeaders = { "X-Tenant-Id": localStorage.getItem("chaosplus.tenant") || "platform" };
  const members = await fetch("/api/iam/members", { credentials: "include", headers: baseHeaders });
  if (!members.ok) throw new Error("member list failed: " + members.status);
  const principal = (await members.json()).data.find((item) => item.status === "active")?.subject;
  const results = [];
  for (const locale of ["en-US", "zh-CN", "ms-MY"]) {
    const response = await fetch("/api/iam/relationships", {
      method: "POST",
      credentials: "include",
      headers: { ...baseHeaders, "X-Lang": locale, "Content-Type": "application/json" },
      body: JSON.stringify({
        subject_type: "principal",
        subject_id: principal,
        relation: "viewer",
        resource_type: "store",
        resource_id: ${JSON.stringify(child.id)},
        condition: {
          version: 1,
          eq: [{ context: "request.header.x-role" }, { value: "admin" }],
        },
      }),
    });
    results.push({ locale, status: response.status, body: await response.json() });
  }
  return results;
`)
if (
  localizedConditionErrors.some(
    (result) =>
      result.status !== 422 ||
      !result.body.message ||
      result.body.message === "invalid_relationship_condition"
  ) ||
  new Set(localizedConditionErrors.map((result) => result.body.message))
    .size !== 3
)
  throw new Error(
    `relationship condition localization failed: ${JSON.stringify(localizedConditionErrors)}`
  )
const expectedConditionStatuses = responses.filter(
  (response) =>
    response.status === 422 &&
    response.method === "POST" &&
    response.url.endsWith("/api/iam/relationships") &&
    !expectedWindowStatuses.includes(response)
)
if (expectedConditionStatuses.length !== 3)
  throw new Error(
    `expected three localized condition failures, got ${expectedConditionStatuses.length}`
  )
await selectOption("#entity-binding-effect", "拒绝")
await setInput(
  "#entity-binding-expiry",
  new Date(Date.now() + 2 * 86400000).toISOString().slice(0, 16)
)
await submit("entity-binding-form")
await waitFor(
  `[...document.querySelectorAll('[role="dialog"] button')].some((item) => item.title === "删除角色绑定")`
)
await waitFor(
  `document.querySelector("[role=dialog]")?.textContent.includes("拒绝")`
)
await selectOption("#authorization-inspection-permission", "store_view")
await setInput("#authorization-inspection-resource", businessResourceID)
await evaluate(`
  const button = [...document.querySelectorAll('[role="dialog"] button')].find(
    (item) => item.textContent.trim() === "计算授权"
  );
  if (!(button instanceof HTMLButtonElement) || button.disabled)
    throw new Error("authorization inspection button unavailable");
  button.click();
`)
await waitFor(`document.querySelector("[data-authorization-explanation]")`)
const authorizationInspection = await evaluate(`
  const result = document.querySelector("[data-authorization-explanation]");
  result?.scrollIntoView({ block: "nearest" });
  return { text: result?.textContent ?? "" };
`)
if (
  !authorizationInspection.text.includes("拒绝") ||
  !authorizationInspection.text.includes("命中显式拒绝") ||
  !authorizationInspection.text.includes("策略版本") ||
  !authorizationInspection.text.includes(businessResourceID) ||
  !authorizationInspection.text.includes("viewer")
)
  throw new Error(
    `authorization explanation is incomplete: ${authorizationInspection.text}`
  )
if (
  !responses.some(
    (response) =>
      response.status === 200 &&
      response.url.endsWith("/api/iam/authorization/constraints")
  ) ||
  !responses.some(
    (response) =>
      response.status === 200 &&
      response.url.endsWith("/api/iam/authorization/explain")
  )
)
  throw new Error("authorization inspection APIs were not completed")

const mobileLayout = await screenshot("admin-entities-mobile.png", 390, 844)
await command("Emulation.setDeviceMetricsOverride", {
  width: 1440,
  height: 1000,
  deviceScaleFactor: 1,
  mobile: false,
})
await evaluate(`
  const close = [...document.querySelectorAll('[role="dialog"] button')].find((button) => button.textContent.trim() === "关闭");
  if (!(close instanceof HTMLButtonElement)) throw new Error("binding close button not found");
  close.click();
`)
await waitFor(`!document.querySelector("#entity-binding-form")`)

await clickRowAction(updatedName, "删除空实体")
await waitFor(`document.querySelector('[role="alert"]')`)
await clickRowAction(updatedName, "角色绑定")
await waitFor(`document.querySelector("#entity-binding-form")`)
await evaluate(`
  const button = [...document.querySelectorAll('[role="dialog"] tbody button')].find((item) => item.title === "删除角色绑定");
  if (!(button instanceof HTMLButtonElement)) throw new Error("binding delete button not found");
  button.click();
`)
await waitFor(
  `![...document.querySelectorAll('[role="dialog"] button')].some((item) => item.title === "删除角色绑定")`
)
await waitFor(
  `[...document.querySelectorAll('[role="dialog"] button')].some((item) => item.textContent.trim() === "计算授权" && !item.disabled)`
)
await selectOption("#authorization-inspection-permission", "store_view")
await setInput("#authorization-inspection-resource", businessResourceID)
await evaluate(`
  const button = [...document.querySelectorAll('[role="dialog"] button')].find(
    (item) => item.textContent.trim() === "计算授权"
  );
  if (!(button instanceof HTMLButtonElement) || button.disabled)
    throw new Error("authorization inspection button unavailable after deny removal");
  button.click();
`)
await waitFor(
  `document.querySelector("[data-authorization-explanation]")?.textContent.includes("→") && !document.querySelector("[data-authorization-explanation]")?.textContent.includes("命中显式拒绝")`
)
const relationshipInspection = await evaluate(`
  return document.querySelector("[data-authorization-explanation]")?.textContent ?? "";
`)
if (
  !relationshipInspection.includes("owner") ||
  !relationshipInspection.includes("viewer") ||
  !relationshipInspection.includes(businessResourceID)
)
  throw new Error(`relationship path is incomplete: ${relationshipInspection}`)
await evaluate(`
  const row = [...document.querySelectorAll('[role="dialog"] tbody tr')].find((item) => item.textContent.includes(${JSON.stringify(businessResourceID)}));
  const button = row?.querySelector('button[title="撤销关系"]');
  if (!(button instanceof HTMLButtonElement)) throw new Error("business resource relationship delete button not found");
  button.click();
`)
await waitFor(
  `!document.querySelector('[role="dialog"]')?.textContent.includes(${JSON.stringify(businessResourceID)})`
)
await waitFor(
  `[...document.querySelectorAll('[role="dialog"] button')].some((item) => item.textContent.trim() === "计算授权" && !item.disabled)`
)
await selectOption("#authorization-inspection-permission", "store_view")
await evaluate(`
  const button = [...document.querySelectorAll('[role="dialog"] button')].find(
    (item) => item.textContent.trim() === "计算授权"
  );
  if (!(button instanceof HTMLButtonElement) || button.disabled) throw new Error("authorization inspection button missing after revoke");
  button.click();
`)
await waitFor(
  `document.querySelector("[data-authorization-explanation]") && !document.querySelector("[data-authorization-explanation]")?.textContent.includes("→")`
)
await setInput("#authorization-inspection-resource", "")
await evaluate(`
  const button = [...document.querySelectorAll('[role="dialog"] button')].find(
    (item) => item.textContent.trim() === "计算授权"
  );
  if (!(button instanceof HTMLButtonElement) || button.disabled) throw new Error("entity authorization inspection button missing");
  button.click();
`)
await waitFor(
  `document.querySelector("[data-authorization-explanation]")?.textContent.includes("→")`
)
await evaluate(`
  const button = [...document.querySelectorAll('[role="dialog"] button')].find((item) => item.title === "撤销关系");
  if (!(button instanceof HTMLButtonElement)) throw new Error("entity relationship delete button not found");
  button.click();
`)
await waitFor(
  `![...document.querySelectorAll('[role="dialog"] button')].some((item) => item.title === "撤销关系")`
)
await evaluate(`
  const button = [...document.querySelectorAll('[role="dialog"] button')].find(
    (item) => item.textContent.trim() === "计算授权"
  );
  if (!(button instanceof HTMLButtonElement) || button.disabled) throw new Error("authorization inspection button missing after entity revoke");
  button.click();
`)
await waitFor(
  `document.querySelector("[data-authorization-explanation]") && !document.querySelector("[data-authorization-explanation]")?.textContent.includes("→")`
)
await evaluate(`
  const close = [...document.querySelectorAll('[role="dialog"] button')].find((button) => button.textContent.trim() === "关闭");
  close.click();
`)
await waitFor(`!document.querySelector("#entity-binding-form")`)

await clickRowAction(rootName, "角色绑定")
await waitFor(`document.querySelector("#entity-relationship-form")`)
await evaluate(`
  const button = [...document.querySelectorAll('[role="dialog"] button')].find((item) => item.title === "撤销关系");
  if (!(button instanceof HTMLButtonElement)) throw new Error("direct relationship delete button not found");
  button.click();
`)
await waitFor(
  `![...document.querySelectorAll('[role="dialog"] button')].some((item) => item.title === "撤销关系")`
)
await evaluate(`
  const close = [...document.querySelectorAll('[role="dialog"] button')].find((button) => button.textContent.trim() === "关闭");
  close.click();
`)
await waitFor(`!document.querySelector("#entity-relationship-form")`)

const desktopLayout = await screenshot("admin-entities-desktop.png", 1440, 1000)
await clickRowAction(updatedName, "删除空实体")
await waitFor(`!(${rowExpression(updatedName)})`)
await clickRowAction(rootName, "删除空实体")
await waitFor(`!(${rowExpression(rootName)})`)

state = await entities()
if (state.some((item) => item.id === root.id || item.id === child.id))
  throw new Error("entity cleanup did not complete")

const auditEvents = await evaluate(`
  const response = await fetch("/api/iam/audit-events?target_id=${encodeURIComponent(child.id)}&limit=100", {
    credentials: "include",
    headers: { "X-Tenant-Id": localStorage.getItem("chaosplus.tenant") || "platform" },
  });
  if (!response.ok) throw new Error("audit list failed: " + response.status);
  return (await response.json()).data.map((event) => event.event_type);
`)
for (const eventType of [
  "entity_created",
  "entity_updated",
  "entity_role_binding_put",
  "entity_role_binding_deleted",
  "relationship_put",
  "relationship_deleted",
  "entity_deleted",
])
  if (!auditEvents.includes(eventType))
    throw new Error(`missing audit event: ${eventType}`)

const expectedConflicts = responses.filter(
  (response) =>
    response.status === 409 &&
    response.method === "DELETE" &&
    response.url.includes("/api/iam/entities/")
)
if (expectedConflicts.length !== 2)
  throw new Error(
    `expected two deletion conflicts, got ${expectedConflicts.length}`
  )
const unexpectedStatuses = responses.filter(
  (response) =>
    response.status >= 400 &&
    !(response.status === 401 && response.url.endsWith("/api/authn/session")) &&
    !expectedWindowStatuses.includes(response) &&
    !expectedConditionStatuses.includes(response) &&
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
    invalidMetadataRejectedBeforeRequest: true,
    hierarchyCreatedAndCollapsed: true,
    entityUpdated: true,
    denyBindingCreatedAndRemoved: true,
    relationshipCreatedExplainedAndRevoked: true,
    relationshipWindowLocalized: true,
    relationshipConditionLocalized: true,
    authorizationConstraintAndExplanationInspected: true,
    deletionGuards: expectedConflicts.length,
    auditEvents: [...new Set(auditEvents)].sort(),
    cleanupCompleted: true,
    desktopLayout,
    mobileLayout,
    consoleErrors,
    failedRequests: unexpectedFailures,
    unexpectedStatuses,
  })
)
