# WASM 插件系统

Chaosplus 支持通过签名 WebAssembly 模块扩展签发的 JWT claims。插件在签发
access token / ID token 时执行：输入是本次签发的上下文，输出是额外的 claims，
会被合并进 token 的 `ext` 字段（以插件名作为键）。

## 适用场景

- 在 token 中加入自定义声明（如 `region`、`membership_tier`、内部属性）。
- 需要完全确定性、可审计、可被信任的扩展逻辑。

插件不能做**任何** IO：没有文件、网络、系统调用。它只能收到 JSON 输入并返回
JSON 输出。想要做 IO 的功能应该由平台接口提供，而不是放在插件里。

## 配置

```yaml
plugins:
  enabled: true
  manifests:
    - /etc/chaosplus/plugins/region.json
  trusted_keys:
    prod: "Base64Ed25519PublicKey=="
  allow_unsigned: false   # 仅开发环境可开；生产必须 false
```

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `enabled` | `false` | 开关。开启后 `manifests` 必须至少一项 |
| `manifests` | `[]` | manifest JSON 文件路径，按顺序加载并执行 |
| `trusted_keys` | `{}` | `key_id -> base64 Ed25519 公钥`，用于验签 |
| `allow_unsigned` | `false` | 允许无签名 manifest；仅限开发 |

## Manifest

每个插件由一个 JSON manifest 和一个 `.wasm` 模块组成。模块路径**必须相对
manifest 所在目录**，且不能逃逸出该目录。

```json
{
  "name": "region_hint",
  "version": "1.0.0",
  "abi": 1,
  "hook": "claims",
  "module": "region_hint.wasm",
  "sha256": "<模块文件 SHA-256 十六进制小写>",
  "key_id": "prod",
  "signature": "<base64 Ed25519 签名>",
  "failure_mode": "deny",
  "timeout_ms": 50,
  "memory_pages": 256,
  "max_output_bytes": 65536
}
```

| 字段 | 约束 | 默认 |
| --- | --- | --- |
| `name` | `^[a-z][a-z0-9_]{0,63}$` | — |
| `version` | 非空 | — |
| `abi` | 必须为 `1` | — |
| `hook` | 必须为 `claims` | — |
| `module` | 相对路径，不可逃逸 manifest 目录 | — |
| `sha256` | 64 位十六进制；必须等于模块文件真实摘要 | — |
| `key_id` | 必须存在于 `trusted_keys`（有签名时） | — |
| `signature` | base64；`allow_unsigned=false` 时必须提供 | — |
| `failure_mode` | `deny` \| `ignore` | `deny` |
| `timeout_ms` | `0..1000` | `50` |
| `memory_pages` | `1..1024`（每页 64KiB） | `256`（16MiB） |
| `max_output_bytes` | `1..65536` | `65536` |

加载器会拒绝：未知字段、非法 `name/version/abi/hook`、模块缺失或超过 16MiB、
摘要不匹配、签名无效、不受信任的 `key_id`、越界的超时/内存/输出上限。

## 签名

Ed25519 签名载荷是下面 5 行的换行拼接（小写 sha256）：

```text
{name}
{version}
{abi}
{hook}
{sha256}
```

签名示例（Go）：

```go
payload := []byte(strings.Join([]string{name, version, "1", "claims", sha256}, "\n"))
signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
```

`trusted_keys` 中配置的公钥通过 `key_id` 对应。公钥和签名都支持
RawStd/Std base64 两种编码。

## WASM ABI

模块必须是**不导入任何 WASI/host 函数**的裸 wasm32 模块，导出：

| 导出 | 签名 | 说明 |
| --- | --- | --- |
| `memory` | — | 线性内存，容量受 `memory_pages` 限制 |
| `alloc` | `(i32) -> i32` | 分配 `size` 字节，返回指针 |
| `claims` | `(i32, i32) -> i64` | 处理输入，返回打包结果 |
| `dealloc` | `(i32, i32)` | 可选；释放输入和输出 |

`claims(ptr, len)` 的返回值是 i64：高 32 位是输出指针，低 32 位是输出长度。
调用流程：宿主 `alloc` 输入长度 → 把输入 JSON 写入内存 → 调用 `claims` →
读取并校验输出（超过 `max_output_bytes` 直接失败）→ 释放输入和输出。

输入是本次签发上下文：

```json
{
  "token_type": "access_token",
  "subject": "principal-id",
  "tenant_id": "tenant-id",
  "audience": "api",
  "scope": "openid profile"
}
```

输出必须是一个 JSON 对象：最多 32 个键，键符合 `^[a-z][a-z0-9_]{0,63}$`；
值只允许 `null`、布尔、字符串、数字和最多 64 个元素的数组（可递归嵌套）。
任何违反都会导致插件失败。

## 构建示例

以下 C 源码把常量 JSON 输出到宿主分配的输入缓冲（输入本身可以忽略），
返回 `(指针 << 32) | 长度`：

```c
#include <string.h>

static const char out[] = "{\"region\":\"us\"}";

__attribute__((export_name("alloc")))
int alloc(int size) { return 0; }

__attribute__((export_name("dealloc")))
void dealloc(int ptr, int size) {}

__attribute__((export_name("claims")))
long long claims(int ptr, int len) {
  memcpy((void *)(unsigned)ptr, out, sizeof(out));
  return ((long long)(unsigned)ptr << 32) | (unsigned long)(sizeof(out) - 1);
}
```

```bash
clang --target=wasm32-unknown-unknown -nostdlib -Wl,--no-entry \
  -Wl,--export=alloc -Wl,--export=claims -Wl,--export=dealloc \
  -o region_hint.wasm region_hint.c
```

要点：

- 模块必须**不导入任何 WASI/host 函数**。不要用 `wasm32-wasi` / `wasip1`
  target，运行时没有注册 WASI host 函数，带导入的模块无法实例化。
- `alloc` 返回输入缓冲指针；宿主把输入 JSON 写进去后调用 `claims`。
- `claims` 的返回值高 32 位是输出指针、低 32 位是输出长度。输出超限、
  指针越界或调用超时都会按 `failure_mode` 处理。
- 也可以手写 WAT 或使用任何能产出无导入 wasm32 模块的工具链，ABI 不变。
## 失败语义

| `failure_mode` | 行为 |
| --- | --- |
| `deny`（默认） | 插件失败（超时、崩溃、输出非法）→ token 签发失败，整体拒绝 |
| `ignore` | 插件失败 → 记录警告，跳过该插件的输出，token 正常签发 |

## 输出位置

插件返回的 claims 以插件名为键合并进 JWT `ext` 字段：

```json
{
  "sub": "...",
  "ext": {
    "region_hint": { "region": "us" }
  }
}
```

`failure_mode=deny` 保证 token 里永远不会出现半成品扩展数据。
