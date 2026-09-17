// maskApiKey hides the secret part of an API key while keeping its prefix recognisable.
//
// Keys minted by CPA-Helper are `<prefix>-<random>` where the random alphabet never contains
// `-`, so the LAST `-` reliably separates the (possibly multi-segment, configurable) prefix
// from the secret — `sk-…`, `sk-myteam-…`, or any custom prefix all mask correctly without the
// UI having to know the configured prefix. Keys with no `-` (foreign / observed keys) fall back
// to a short fixed head. The output always has the same length as the input, and at least 8
// characters are masked for any key longer than 12.
export function maskApiKey(apiKey: string): string {
  if (apiKey.length <= 12) {
    return `${apiKey.slice(0, 3)}${'*'.repeat(Math.max(apiKey.length - 3, 0))}`
  }
  const visibleSuffix = 4
  const minMasked = 8
  const dash = apiKey.lastIndexOf('-')
  const wantedPrefix = dash > 0 ? dash + 1 : 6
  const maxPrefix = apiKey.length - visibleSuffix - minMasked
  const visiblePrefix = Math.max(1, Math.min(wantedPrefix, maxPrefix))
  const maskedLength = apiKey.length - visiblePrefix - visibleSuffix
  return `${apiKey.slice(0, visiblePrefix)}${'*'.repeat(maskedLength)}${apiKey.slice(-visibleSuffix)}`
}
