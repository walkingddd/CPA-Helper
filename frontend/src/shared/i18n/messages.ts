import { currentLanguage, localize } from './language'

type MessagePair = readonly [zh: string, en: string]
type ServerMessagePattern = readonly [RegExp, (match: RegExpMatchArray) => string]

const exactServerMessages: MessagePair[] = [
  ['请求失败', 'Request failed'],
  ['复制失败', 'Copy failed'],
  ['没有可复制的内容', 'Nothing to copy'],
  ['复制失败，请手动选中后复制', 'Copy failed. Select the text and copy it manually.'],
  ['登录状态缺少角色信息，请重启后端服务后重新登录', 'Your session is missing role information. Restart the backend and sign in again.'],
  ['CPA 配置未完成：请先到「系统设置」填写 CLIProxyAPI 地址和管理密钥，再返回 API 密钥页操作。', 'CPA settings are incomplete. Fill in the CLIProxyAPI URL and management key in System Settings, then return to API Keys.'],
  ['服务器内部错误', 'Internal server error'],
  ['CLIProxyAPI 未确认重置成功（响应缺少 status=ok 或 auth_index 不匹配）', 'CLIProxyAPI did not confirm the reset (response missing status=ok or auth_index mismatch)'],
  ['该账号缺少 auth_index，请先刷新账号列表', 'This account has no auth_index yet; refresh the account list first'],
  ['auth_name 不能为空', 'auth_name must not be empty'],
  ['账号不存在', 'Account not found'],
  ['请求体不是有效 JSON', 'Request body is not valid JSON'],
  ['请先登录', 'Sign in first'],
  ['登录会话已失效', 'Your sign-in session has expired'],
  ['首次登录后必须先修改账号密码', 'Change the account password after first sign-in'],
  ['需要管理员权限', 'Admin access is required'],
  ['资源不存在', 'Resource does not exist'],
  ['用户名或密码不正确', 'Username or password is incorrect'],
  ['系统尚未初始化，请先创建第一个管理员账号', 'The system is not initialized. Create the first admin account first.'],
  ['第一个管理员账号已存在', 'The first admin account already exists'],
  ['需要提供当前密码', 'Current password is required'],
  ['当前密码不正确', 'Current password is incorrect'],
  ['请先创建第一个管理员账号', 'Create the first admin account first'],
  ['尚未运行', 'Not run yet'],
  ['正在运行多个 Codex Keeper 任务', 'Multiple Codex Keeper tasks are running'],
  ['正在刷新 Codex 账号', 'Refreshing Codex accounts'],
  ['正在按条件刷新 Codex 账号', 'Refreshing Codex accounts by condition'],
  ['正在巡检 Codex 账号', 'Inspecting Codex accounts'],
  ['巡检完成', 'Inspection complete'],
  ['缓存时间内没有需要自动刷新的 Codex auth file', 'No Codex auth files need automatic refresh inside the cache window'],
  ['未发现指定 Codex auth file', 'No matching Codex auth file was found'],
  ['未发现 Codex auth file', 'No Codex auth files were found'],
  ['缺少 access token', 'Missing access token'],
  ['读取 auth file 详情失败', 'Failed to read auth file details'],
  ['管理密钥未设置，无法运行 Codex Keeper', 'Management key is not set, so Codex Keeper cannot run'],
  ['Codex Keeper 已开始按计划自动巡检', 'Codex Keeper scheduled automatic inspection started'],
  ['Codex Keeper 已停止自动巡检', 'Codex Keeper automatic inspection stopped'],
]

const serverCodeFallbackMessages = new Map<string, MessagePair>([
  ['app_error', ['服务器内部错误', 'Internal server error']],
  ['authentication_failed', ['请先登录', 'Sign in first']],
  ['conflict', ['请求冲突', 'Request conflict']],
  ['forbidden', ['需要管理员权限', 'Admin access is required']],
  ['method_not_allowed', ['请求方法不支持', 'Request method is not allowed']],
  ['not_found', ['资源不存在', 'Resource does not exist']],
  ['startup_check_failed', ['启动检查失败', 'Startup check failed']],
  ['validation_error', ['请求参数无效', 'Invalid request parameters']],
])

const serverTermTranslations: MessagePair[] = [
  ['CLIProxyAPI 管理请求', 'CLIProxyAPI management request'],
  ['CLIProxyAPI 地址和管理密钥', 'CLIProxyAPI URL and management key'],
  ['CLIProxyAPI 地址', 'CLIProxyAPI URL'],
  ['管理密钥', 'management key'],
  ['Codex Keeper 自动启动配置', 'Codex Keeper auto-start settings'],
  ['Codex Keeper 自动巡检', 'Codex Keeper automatic inspection'],
  ['Codex Keeper 定时表达式', 'Codex Keeper schedule expression'],
  ['Codex Keeper 条件刷新配置', 'Codex Keeper conditional refresh settings'],
  ['Codex Keeper 条件刷新候选', 'Codex Keeper conditional refresh candidates'],
  ['Codex Keeper 配置', 'Codex Keeper settings'],
  ['Codex Keeper', 'Codex Keeper'],
  ['Codex auth file', 'Codex auth file'],
  ['Codex auth files', 'Codex auth files'],
  ['auth file 详情', 'auth file details'],
  ['auth files', 'auth files'],
  ['auth file', 'auth file'],
  ['CPA 配置', 'CPA settings'],
  ['CPA API KEY', 'CPA API key'],
  ['CPA 可用模型', 'CPA available models'],
  ['CPA 当前模型', 'CPA current models'],
  ['CPA 模型列表', 'CPA model list'],
  ['API KEY 标识', 'API key identifier'],
  ['API KEY 描述', 'API key description'],
  ['API KEY 绑定', 'API key binding'],
  ['API KEY', 'API key'],
  ['API Key', 'API key'],
  ['usage 记录', 'usage record'],
  ['usage 检测', 'usage check'],
  ['usage 开关', 'usage switch'],
  ['api-call 管理请求', 'api-call management request'],
  ['api-call 响应', 'api-call response'],
  ['模型请求地址', 'model request URL'],
  ['模型请求', 'model request'],
  ['模型响应', 'model response'],
  ['模型列表响应字段', 'model list response field'],
  ['模型列表响应', 'model list response'],
  ['模型列表', 'model list'],
  ['模型价格', 'model prices'],
  ['请求地址', 'request URL'],
  ['请求格式', 'request format'],
  ['请求体', 'request body'],
  ['测试模型名称', 'test model name'],
  ['测试模型', 'test model'],
  ['测试消息', 'test message'],
  ['远程 usage 开关', 'remote usage switch'],
  ['远程', 'remote'],
  ['代理地址', 'proxy URL'],
  ['价格数据', 'price data'],
  ['账号名称', 'account name'],
  ['账号类型', 'account type'],
  ['账号状态', 'account status'],
  ['账号巡检', 'account inspection'],
  ['账号和密码', 'account and password'],
  ['账号和昵称', 'account and nickname'],
  ['账号', 'account'],
  ['用户名', 'username'],
  ['昵称', 'nickname'],
  ['密码长度', 'password length'],
  ['密码', 'password'],
  ['第一个管理员账号', 'first admin account'],
  ['第一个用户', 'first user'],
  ['用户额度', 'user quota'],
  ['用户列表', 'users'],
  ['用户', 'user'],
  ['当前密码', 'current password'],
  ['登录会话', 'sign-in session'],
  ['完整密钥', 'full key'],
  ['原始 API KEY', 'original API key'],
  ['本地已不存在的 Codex 账号', 'local Codex accounts that no longer exist'],
  ['Codex 账号', 'Codex accounts'],
  ['坏凭证', 'bad credential'],
  ['凭证', 'credential'],
  ['网络检测', 'network check'],
  ['网络错误', 'network errors'],
  ['额度使用率', 'quota usage'],
  ['额度金额', 'quota amount'],
  ['额度', 'quota'],
  ['优先级降级', 'priority lowered'],
  ['优先级恢复', 'priority restored'],
  ['优先级', 'priority'],
  ['类型优先级', 'type priority'],
  ['巡检账号历史', 'account inspection history'],
  ['巡检', 'inspection'],
  ['条件刷新', 'conditional refresh'],
  ['账号刷新', 'account refresh'],
  ['刷新', 'refresh'],
  ['恢复启用', 'restored enabled state'],
  ['启用', 'enable'],
  ['禁用', 'disable'],
  ['缓存', 'cache'],
  ['健康', 'healthy'],
  ['跳过', 'skipped'],
  ['系统设置', 'system settings'],
  ['设置', 'settings'],
  ['账户', 'account'],
  ['可用模型', 'available models'],
  ['代理配置', 'proxy settings'],
  ['API 密钥', 'API keys'],
  ['历史用量', 'usage history'],
  ['明细', 'records'],
  ['原始数据', 'raw data'],
  ['巡检配置', 'inspection settings'],
  ['正在运行', 'running'],
  ['正在其他 Keeper 任务处理中', 'being processed by another Keeper task'],
  ['无效', 'invalid'],
  ['不存在', 'does not exist'],
  ['已存在', 'already exists'],
  ['不能为空', 'is required'],
  ['不能超过', 'cannot exceed'],
  ['不能少于', 'must be at least'],
  ['不能小于', 'cannot be less than'],
  ['必须是', 'must be'],
  ['不是有效', 'is not valid'],
  ['格式不支持', 'format is unsupported'],
  ['响应缺少', 'response is missing'],
  ['超出范围', 'is out of range'],
  ['下载', 'download'],
  ['查询', 'query'],
  ['读取', 'read'],
  ['写入', 'write'],
  ['保存', 'save'],
  ['删除', 'delete'],
  ['同步', 'sync'],
  ['更新', 'update'],
  ['创建', 'create'],
  ['失败', 'failed'],
  ['成功', 'succeeded'],
  ['完成', 'complete'],
  ['：', ': '],
  ['，', ', '],
  ['；', '; '],
  ['。', '.'],
]

const serverMessagePatterns: ServerMessagePattern[] = [
  [/^操作失败$/, () => 'Operation failed'],
  [/^加载(.+)失败$/, ([, subject]) => `Failed to load ${translateTerms(subject ?? '')}`],
  [/^保存(.+)失败$/, ([, subject]) => `Failed to save ${translateTerms(subject ?? '')}`],
  [/^删除(.+)失败$/, ([, subject]) => `Failed to delete ${translateTerms(subject ?? '')}`],
  [/^同步(.+)失败$/, ([, subject]) => `Failed to sync ${translateTerms(subject ?? '')}`],
  [/^更新(.+)失败$/, ([, subject]) => `Failed to update ${translateTerms(subject ?? '')}`],
  [/^创建(.+)失败$/, ([, subject]) => `Failed to create ${translateTerms(subject ?? '')}`],
  [/^(.+)和(.+)不能为空$/, ([, left, right]) => `${translateTerms(left ?? '')} and ${translateTerms(right ?? '')} are required`],
  [/^(.+)或(.+)不能为空$/, ([, left, right]) => `${translateTerms(left ?? '')} or ${translateTerms(right ?? '')} is required`],
  [/^(.+)不能为空$/, ([, subject]) => `${translateTerms(subject ?? '')} is required`],
  [/^(.+)不能超过 (\d+) 个字符$/, ([, subject, count]) => `${translateTerms(subject ?? '')} cannot exceed ${count} characters`],
  [/^(.+)不能少于 (\d+) 位$/, ([, subject, count]) => `${translateTerms(subject ?? '')} must be at least ${count} characters`],
  [/^(.+)不能小于 (\d+)$/, ([, subject, count]) => `${translateTerms(subject ?? '')} cannot be less than ${count}`],
  [/^(.+)超出范围$/, ([, subject]) => `${translateTerms(subject ?? '')} is out of range`],
  [/^(.+)无效$/, ([, subject]) => `${translateTerms(subject ?? '')} is invalid`],
  [/^(.+)不存在$/, ([, subject]) => `${translateTerms(subject ?? '')} does not exist`],
  [/^(.+)已存在$/, ([, subject]) => `${translateTerms(subject ?? '')} already exists`],
  [/^(.+)不是有效 JSON$/, ([, subject]) => `${translateTerms(subject ?? '')} is not valid JSON`],
  [/^(.+)响应不是有效 JSON$/, ([, subject]) => `${translateTerms(subject ?? '')} response is not valid JSON`],
  [/^(.+)响应格式不支持$/, ([, subject]) => `${translateTerms(subject ?? '')} response format is unsupported`],
  [/^(.+)响应缺少(.+)$/, ([, subject, field]) => `${translateTerms(subject ?? '')} response is missing ${translateTerms(field ?? '')}`],
  [/^(.+)请求失败：HTTP (\d+)$/, ([, subject, status]) => `${translateTerms(subject ?? '')} request failed: HTTP ${status}`],
  [/^(.+)请求失败：(.+)$/, ([, subject, detail]) => `${translateTerms(subject ?? '')} request failed: ${translateNested(detail ?? '')}`],
  [/^(.+)失败：HTTP (\d+)：(.+)$/, ([, subject, status, detail]) => `${translateTerms(subject ?? '')} failed: HTTP ${status}: ${translateNested(detail ?? '')}`],
  [/^(.+)失败：HTTP (\d+)$/, ([, subject, status]) => `${translateTerms(subject ?? '')} failed: HTTP ${status}`],
  [/^(.+)失败：(.+)$/, ([, subject, detail]) => `${translateTerms(subject ?? '')} failed: ${translateNested(detail ?? '')}`],
  [/^(.+)失败$/, ([, subject]) => `${translateTerms(subject ?? '')} failed`],
  [/^(.+)超时，请检查(.+)$/, ([, subject, detail]) => `${translateTerms(subject ?? '')} timed out. Check ${translateTerms(detail ?? '')}`],
  [/^URL 必须是有效的 HTTP\/HTTPS 地址$/, () => 'URL must be a valid HTTP/HTTPS URL'],
  [/^(.+)必须是有效的 http:\/\/ 或 https:\/\/ 地址$/, ([, subject]) => `${translateTerms(subject ?? '')} must be a valid http:// or https:// URL`],
  [/^(.+)必须使用 http:\/\/ 或 https:\/\/$/, ([, subject]) => `${translateTerms(subject ?? '')} must use http:// or https://`],
  [/^(.+)不能包含查询参数或锚点$/, ([, subject]) => `${translateTerms(subject ?? '')} cannot contain query parameters or anchors`],
  [/^(.+)必须是有效的 http:\/\/、https:\/\/ 或 socks5:\/\/ 地址$/, ([, subject]) => `${translateTerms(subject ?? '')} must be a valid http://, https://, or socks5:// URL`],
  [/^当前 (.+) 缺少完整密钥，无法发起测试$/, ([, subject]) => `The current ${translateTerms(subject ?? '')} is missing the full key and cannot start a test`],
  [/^用户额度已用尽，API KEY 已暂停，请联系管理员补充额度$/, () => 'User quota is exhausted. API keys are paused; contact an admin to add quota.'],
  [/^用户额度已用尽，请补充额度后再恢复 API KEY$/, () => 'User quota is exhausted. Add quota before restoring API keys.'],
  [/^存在无法恢复的 API KEY，请重新绑定后再启用$/, () => 'Some API keys cannot be restored. Rebind them before enabling the user.'],
  [/^存在无法恢复的 API KEY，请重新绑定后再恢复$/, () => 'Some API keys cannot be restored. Rebind them before restoring.'],
  [/^API KEY 与 API KEY 标识不匹配$/, () => 'API key does not match the API key identifier'],
  [/^API KEY 或 API KEY 标识不能为空$/, () => 'API key or API key identifier is required'],
  [/^未找到完整 API KEY，请粘贴原始 API KEY$/, () => 'Full API key was not found. Paste the original API key.'],
  [/^生成 API KEY 失败，请重试$/, () => 'Failed to generate API key. Try again.'],
  [/^第一个用户不能禁用$/, () => 'The first user cannot be disabled'],
  [/^账号不允许修改$/, () => 'Account cannot be changed'],
  [/^第一个管理员账号不能取消管理员权限$/, () => 'The first admin account cannot lose admin access'],
  [/^用户已禁用$/, () => 'User is disabled'],
  [/^当前 API KEY 缺少完整密钥，无法发起测试$/, () => 'The current API key is missing the full key and cannot start a test'],
  [/^模型请求失败：HTTP (\d+)：(.+)$/, ([, status, detail]) => `Model request failed: HTTP ${status}: ${translateNested(detail ?? '')}`],
  [/^模型请求失败：HTTP (\d+)$/, ([, status]) => `Model request failed: HTTP ${status}`],
  [/^模型请求失败：(.+)$/, ([, detail]) => `Model request failed: ${translateNested(detail ?? '')}`],
  [/^模型请求超时，请检查模型请求地址或稍后重试$/, () => 'Model request timed out. Check the model request URL or try again later.'],
  [/^模型响应不是有效 JSON$/, () => 'Model response is not valid JSON'],
  [/^请求格式不支持$/, () => 'Request format is unsupported'],
  [/^查询 CPA 可用模型失败：(.+)$/, ([, detail]) => `Failed to query CPA available models: ${translateNested(detail ?? '')}`],
  [/^CPA 模型列表响应字段 (.+) 不是列表$/, ([, field]) => `CPA model list response field ${field} is not a list`],
  [/^CPA 模型列表响应缺少模型列表$/, () => 'CPA model list response is missing the model list'],
  [/^下载 LiteLLM 价格数据失败：HTTP (\d+)$/, ([, status]) => `Failed to download LiteLLM price data: HTTP ${status}`],
  [/^下载 LiteLLM 价格数据失败$/, () => 'Failed to download LiteLLM price data'],
  [/^LiteLLM 价格数据不是有效 JSON$/, () => 'LiteLLM price data is not valid JSON'],
  [/^该 provider\/model 价格已存在$/, () => 'This provider/model price already exists'],
  [/^provider\/model 不能为空$/, () => 'Provider/model is required'],
  [/^价格不能为负数$/, () => 'Price cannot be negative'],
  [/^启用代理时必须填写代理地址$/, () => 'Proxy URL is required when proxy is enabled'],
  [/^Cron 表达式无效，请使用 5 段格式：分 时 日 月 周$/, () => 'Invalid Cron expression. Use the 5-field format: minute hour day month weekday'],
  [/^Cron 表达式无效，请使用 5 段格式$/, () => 'Invalid Cron expression. Use the 5-field format'],
  [/^开始按条件刷新 (\d+) 个 Codex 账号$/, ([, count]) => `Started conditional refresh for ${count} Codex accounts`],
  [/^开始刷新 (\d+) 个 Codex 账号$/, ([, count]) => `Started refreshing ${count} Codex accounts`],
  [/^开始 Codex 账号巡检$/, () => 'Started Codex account inspection'],
  [/^下一轮计划：(.+)$/, ([, time]) => `Next scheduled run: ${time}`],
  [/^清理本地已不存在的 Codex 账号 (\d+) 个$/, ([, count]) => `Cleaned up ${count} local Codex accounts that no longer exist`],
  [/^巡检完成：网络错误 (\d+)$/, ([, count]) => `Inspection complete: ${count} network errors`],
  [/^条件刷新完成：健康 (\d+)，坏凭证禁用 (\d+)，恢复启用 (\d+)，优先级降级 (\d+)，优先级恢复 (\d+)，网络错误 (\d+)，缓存跳过 (\d+)$/, ([, healthy, disabled, restored, degraded, priorityRestored, networkErrors, skipped]) => `Conditional refresh complete: ${healthy} healthy, ${disabled} bad credentials disabled, ${restored} restored, ${degraded} priorities lowered, ${priorityRestored} priorities restored, ${networkErrors} network errors, ${skipped} skipped by cache`],
  [/^账号刷新完成：健康 (\d+)，凭证异常 (\d+)，恢复启用 (\d+)，优先级降级 (\d+)，优先级恢复 (\d+)，网络错误 (\d+)$/, ([, healthy, credentialErrors, restored, degraded, priorityRestored, networkErrors]) => `Account refresh complete: ${healthy} healthy, ${credentialErrors} credential errors, ${restored} restored, ${degraded} priorities lowered, ${priorityRestored} priorities restored, ${networkErrors} network errors`],
  [/^巡检完成：健康 (\d+)，坏凭证禁用 (\d+)，恢复启用 (\d+)，优先级降级 (\d+)，网络错误 (\d+)，缓存跳过 (\d+)$/, ([, healthy, disabled, restored, degraded, networkErrors, skipped]) => `Inspection complete: ${healthy} healthy, ${disabled} bad credentials disabled, ${restored} restored, ${degraded} priorities lowered, ${networkErrors} network errors, ${skipped} skipped by cache`],
  [/^(.+): 巡检正常，类型 (.+)$/, ([, name, accountType]) => `${name}: inspection healthy, type ${accountType}`],
  [/^(.+): 正在其他 Keeper 任务处理中，跳过$/, ([, name]) => `${name}: skipped because another Keeper task is processing it`],
  [/^(.+): 缓存时间内已刷新，跳过$/, ([, name]) => `${name}: skipped because it was refreshed inside the cache window`],
  [/^刷新完成，类型 (.+)$/, ([, accountType]) => `Refresh complete, type ${accountType}`],
  [/^禁用凭证：(.+)$/, ([, detail]) => `Disabled credential: ${translateNested(detail ?? '')}`],
  [/^模拟禁用：(.+)$/, ([, detail]) => `Dry run disable: ${translateNested(detail ?? '')}`],
  [/^刷新发现凭证不可用：(.+)$/, ([, detail]) => `Refresh found unavailable credential: ${translateNested(detail ?? '')}`],
  [/^凭证不可用：HTTP (\d+)$/, ([, status]) => `Credential unavailable: HTTP ${status}`],
  [/^网络检测失败：(.+)$/, ([, detail]) => `Network check failed: ${translateNested(detail ?? '')}`],
  [/^usage 检测失败：HTTP (\d+)$/, ([, status]) => `Usage check failed: HTTP ${status}`],
  [/^恢复启用：usage 检测恢复 HTTP (\d+)$/, ([, status]) => `Restored enabled state: usage check recovered with HTTP ${status}`],
  [/^模拟恢复启用：usage 检测恢复 HTTP (\d+)$/, ([, status]) => `Dry run restore enabled state: usage check recovered with HTTP ${status}`],
  [/^降为低优先级：额度使用率达到阈值 (\d+)%$/, ([, percent]) => `Lowered priority: quota usage reached the ${percent}% threshold`],
  [/^模拟降为低优先级：额度使用率达到阈值 (\d+)%$/, ([, percent]) => `Dry run lower priority: quota usage reached the ${percent}% threshold`],
  [/^恢复优先级：priority (-?\d+)$/, ([, priority]) => `Restored priority: priority ${priority}`],
  [/^模拟恢复优先级：priority (-?\d+)$/, ([, priority]) => `Dry run restore priority: priority ${priority}`],
  [/^应用类型优先级：(.+) -> priority (-?\d+)$/, ([, accountType, priority]) => `Applied type priority: ${accountType} -> priority ${priority}`],
  [/^模拟应用类型优先级：(.+) -> priority (-?\d+)$/, ([, accountType, priority]) => `Dry run apply type priority: ${accountType} -> priority ${priority}`],
  [/^启用 WebSocket 传输失败：(.+)$/, ([, detail]) => `Failed to enable WebSocket transport: ${translateNested(detail ?? '')}`],
  [/^(.+): 已启用 WebSocket 传输$/, ([, name]) => `${name}: enabled WebSocket transport`],
  [/^只能删除已禁用账号$/, () => 'Only disabled accounts can be deleted'],
  [/^只能设置小于 -1、大于 20，或当前账号类型 (.+) 对应的 priority (-?\d+)$/, ([, accountType, priority]) => `Priority must be less than -1, greater than 20, or the current account type ${accountType} priority ${priority}`],
  [/^该账号类型没有可设置的系统 priority$/, () => 'This account type has no system priority that can be set'],
  [/^模拟(.+)$/, ([, action]) => `Dry run: ${translateNested(action ?? '')}`],
  [/^(.+?): (.+)$/, ([, prefix, detail]) => `${prefix}: ${translateNested(detail ?? '')}`],
]

function containsHan(value: string): boolean {
  return /\p{Script=Han}/u.test(value)
}

function translateTerms(value: string): string {
  return serverTermTranslations.reduce(
    (text, [zh, en]) => text.split(zh).join(en),
    value,
  )
}

function translateNested(value: string): string {
  return translateKnownServerMessage(value) ?? translateTerms(value)
}

function translateKnownServerMessage(message: string): string | null {
  const exact = exactServerMessages.find(([zh]) => zh === message)
  if (exact) {
    return exact[1]
  }

  for (const [pattern, translate] of serverMessagePatterns) {
    const match = message.match(pattern)
    if (match) {
      return translate(match)
    }
  }

  const termTranslated = translateTerms(message)
  if (termTranslated !== message) {
    return termTranslated
  }

  return null
}

export function localizedServerMessage(
  message: string,
  fallbackZh = '请求失败',
  fallbackEn = 'Request failed',
): string {
  if (currentLanguage.value === 'zh') {
    return message || fallbackZh
  }

  if (!message) {
    return fallbackEn
  }

  if (!containsHan(message)) {
    return message
  }

  const translated = translateKnownServerMessage(message)
  if (translated) {
    return containsHan(translated) ? `${fallbackEn}: ${translated}` : translated
  }

  return `${fallbackEn}: ${message}`
}

export function localizedApiErrorMessage(
  code: string | null | undefined,
  message: string | null | undefined,
): string {
  const fallback = code ? serverCodeFallbackMessages.get(code) : null
  if (message) {
    return localizedServerMessage(
      message,
      fallback?.[0] ?? '请求失败',
      fallback?.[1] ?? 'Request failed',
    )
  }
  if (fallback) {
    return localizedServerMessage('', fallback[0], fallback[1])
  }
  return localizedServerMessage('', '请求失败', 'Request failed')
}

export function localizedKeeperStatusDetail(message: string | null | undefined): string {
  if (!message) {
    return localize('未运行', 'Not running')
  }
  const normalized = message
    .replace(/守护运行中/g, localize('自动巡检运行中', 'Automatic inspection running'))
    .replace(/守护进程/g, localize('后台自动巡检', 'background automatic inspection'))
    .replace(/守护任务/g, localize('自动巡检任务', 'automatic inspection task'))
    .replace(/守护已启动/g, localize('已开始自动巡检', 'Automatic inspection started'))
  return currentLanguage.value === 'zh'
    ? normalized
    : localizedServerMessage(normalized, '运行状态', 'Run status')
}

export function errorText(
  error: unknown,
  fallbackZh = '请求失败',
  fallbackEn = 'Request failed',
): string {
  const message = error instanceof Error ? error.message : ''
  return localizedServerMessage(message, fallbackZh, fallbackEn)
}

export function copiedText(label: string): string {
  return localize(`${label} 已复制`, `${label} copied`)
}
