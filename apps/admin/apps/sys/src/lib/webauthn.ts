type WebAuthnOptions = {
  publicKey: Record<string, unknown>
  mediation?: CredentialMediationRequirement
}

export async function createPasskeyCredential(
  options: WebAuthnOptions
): Promise<Record<string, unknown>> {
  requireWebAuthn()
  const publicKey = decodeCreationOptions(options.publicKey)
  const credential = await navigator.credentials.create({ publicKey })
  if (!(credential instanceof PublicKeyCredential))
    throw new Error("未创建通行密钥")
  const response = credential.response
  if (!(response instanceof AuthenticatorAttestationResponse))
    throw new Error("通行密钥响应无效")
  return {
    id: credential.id,
    rawId: encodeBase64URL(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      attestationObject: encodeBase64URL(response.attestationObject),
      clientDataJSON: encodeBase64URL(response.clientDataJSON),
      transports: response.getTransports(),
    },
  }
}

export async function getPasskeyCredential(
  options: WebAuthnOptions
): Promise<Record<string, unknown>> {
  requireWebAuthn()
  const publicKey = decodeRequestOptions(options.publicKey)
  const credential = await navigator.credentials.get({
    publicKey,
    mediation: options.mediation,
  })
  if (!(credential instanceof PublicKeyCredential))
    throw new Error("未选择通行密钥")
  const response = credential.response
  if (!(response instanceof AuthenticatorAssertionResponse))
    throw new Error("通行密钥响应无效")
  return {
    id: credential.id,
    rawId: encodeBase64URL(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      authenticatorData: encodeBase64URL(response.authenticatorData),
      clientDataJSON: encodeBase64URL(response.clientDataJSON),
      signature: encodeBase64URL(response.signature),
      userHandle: response.userHandle
        ? encodeBase64URL(response.userHandle)
        : null,
    },
  }
}

export function encodeBase64URL(value: ArrayBuffer): string {
  const bytes = new Uint8Array(value)
  let binary = ""
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/, "")
}

export function decodeBase64URL(value: string): ArrayBuffer {
  const normalized = value.replaceAll("-", "+").replaceAll("_", "/")
  const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "=")
  const binary = atob(padded)
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0))
  return bytes.buffer
}

function decodeCreationOptions(
  value: Record<string, unknown>
): PublicKeyCredentialCreationOptions {
  const user = value.user as Record<string, unknown>
  return {
    ...value,
    challenge: decodeBase64URL(String(value.challenge)),
    user: { ...user, id: decodeBase64URL(String(user.id)) },
    excludeCredentials: decodeDescriptors(value.excludeCredentials),
  } as PublicKeyCredentialCreationOptions
}

function decodeRequestOptions(
  value: Record<string, unknown>
): PublicKeyCredentialRequestOptions {
  return {
    ...value,
    challenge: decodeBase64URL(String(value.challenge)),
    allowCredentials: decodeDescriptors(value.allowCredentials),
  } as PublicKeyCredentialRequestOptions
}

function decodeDescriptors(value: unknown): PublicKeyCredentialDescriptor[] {
  if (!Array.isArray(value)) return []
  return value.map((descriptor) => {
    const item = descriptor as Record<string, unknown>
    return { ...item, id: decodeBase64URL(String(item.id)) }
  }) as PublicKeyCredentialDescriptor[]
}

function requireWebAuthn(): void {
  if (
    typeof window === "undefined" ||
    !("PublicKeyCredential" in window) ||
    !navigator.credentials
  )
    throw new Error("当前浏览器不支持通行密钥")
}
