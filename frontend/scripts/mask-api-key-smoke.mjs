import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'

import { createServer } from 'vite'

const root = fileURLToPath(new URL('..', import.meta.url))
const server = await createServer({ root, logLevel: 'error', server: { middlewareMode: true } })

try {
  const { maskApiKey } = await server.ssrLoadModule('/src/shared/utils/maskApiKey.ts')
  const secret = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ' // 52 chars, like generated keys

  // Default prefix: the whole prefix + dash stays visible, secret masked, last 4 kept.
  const sk = maskApiKey(`sk-${secret}`)
  assert.equal(sk.length, `sk-${secret}`.length)
  assert.ok(sk.startsWith('sk-*'), sk)
  assert.ok(sk.endsWith('WXYZ'), sk)
  assert.ok(!sk.includes('abcd'), 'secret head must be masked')

  // Custom multi-segment prefix (the reason this helper exists): `sk-myteam-` stays readable.
  const custom = maskApiKey(`sk-myteam-${secret}`)
  assert.equal(custom.length, `sk-myteam-${secret}`.length)
  assert.ok(custom.startsWith('sk-myteam-*'), custom)
  assert.ok(custom.endsWith('WXYZ'), custom)

  // A prefix containing '_' and digits.
  assert.ok(maskApiKey(`team_42-${secret}`).startsWith('team_42-*'))

  // Foreign key with no dash: short fixed head, still at least 8 masked, same length.
  const foreign = maskApiKey('ABCDEFGHIJKLMNOPQRSTUVWXYZ0123')
  assert.equal(foreign.length, 30)
  assert.equal(foreign.slice(0, 6), 'ABCDEF')
  assert.ok(/^\*{8,}/.test(foreign.slice(6, -4)))

  // A dash placed too late must not expose the secret: prefix is capped so ≥ 8 chars are masked.
  const lateDash = maskApiKey('abcdefghijklmnop-xyz1')
  assert.equal(lateDash.length, 21)
  assert.ok(lateDash.slice(-4) === 'xyz1' && (lateDash.match(/\*/g) || []).length >= 8, lateDash)

  // Short keys keep the legacy 3-visible behaviour.
  assert.equal(maskApiKey('sk-short1234'), 'sk-*********')

  console.log('mask-api-key-smoke: OK')
} finally {
  await server.close()
}
