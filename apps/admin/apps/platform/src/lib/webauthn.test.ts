import { describe, expect, it } from "bun:test"
import { decodeBase64URL, encodeBase64URL } from "./webauthn"

describe("WebAuthn browser encoding", () => {
  it("round trips binary values using unpadded base64url", () => {
    const input = Uint8Array.from([0, 1, 2, 127, 128, 253, 254, 255]).buffer
    const encoded = encodeBase64URL(input)
    expect(encoded).not.toMatch(/[+/=]/)
    expect(Array.from(new Uint8Array(decodeBase64URL(encoded)))).toEqual(
      Array.from(new Uint8Array(input))
    )
  })
})
