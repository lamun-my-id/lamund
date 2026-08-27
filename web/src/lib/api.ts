// Klien API tipis ke /api/v1. Semua data UI lewat sini (jangan akses store langsung).
const TOKEN_KEY = 'lamund.token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}
export function setToken(t: string | null) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  const tok = getToken()
  if (tok) headers['Authorization'] = `Bearer ${tok}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'

  const res = await fetch(`/api/v1${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : {}
  if (!res.ok) {
    throw new ApiError(res.status, data.error || `HTTP ${res.status}`)
  }
  return data as T
}

// ---- tipe ----
export interface DnsRecord {
  id: number
  name: string
  type: string
  value: string
  ttl: number
  priority: number
  zone_id?: number
}

export interface Site {
  domain: string
  type: 'static' | 'proxy'
  proxy_target?: string
  root_path?: string
  status: string
  created_at?: string
  owner_type?: string
  owner_id?: number
  repo_url?: string
  branch?: string
  build_cmd?: string
  output_dir?: string
  dns_auto?: boolean
}
export interface User {
  id: number
  username: string
  role: string
  disabled: boolean
  email?: string
  mfa_enabled?: boolean
  max_sites?: number
  max_teams?: number
  max_memory_mb?: number
  max_cpu_percent?: number
  max_apps?: number
  approval?: string // approved|pending|rejected
}
export interface ApiKey {
  id: number
  name: string
  last_used_at: string
}
export interface Cert {
  domain: string
  issuer: string
  not_after: string
  status: 'valid' | 'expiring' | 'expired' | 'none'
}
export interface SiteFile {
  path: string
  size: number
}
export interface RouteRule {
  path_prefix: string
  type: 'static' | 'proxy' | 'app'
  upstream?: string
  app?: string // untuk type 'app': nama (domain) app yang di-mount
  cache?: boolean
  spa?: boolean
}
export interface Connection {
  provider: string
  connected: boolean
  meta?: { login?: string } | null
}
export interface DomainStat {
  domain: string
  requests: number
  bytes: number
  errors: number
}
export interface ErrorEntry {
  time: string
  domain: string
  path: string
  status: number
}
export interface AnalyticsOverview {
  total_requests: number
  total_bytes: number
  total_errors: number
  top_accessed: DomainStat[]
  recent_errors: ErrorEntry[]
}
export interface HourPoint {
  hour: string
  requests: number
  bytes: number
}
export interface DomainReport {
  domain: string
  requests: number
  bytes: number
  errors: number
  series: HourPoint[]
  recent_errors: ErrorEntry[]
}
export interface Team {
  id: number
  name: string
  slug: string
  role?: string
  member_count?: number
}
export interface TeamMember {
  user_id: number
  username: string
  name: string
  role: string
}
export interface Me {
  id: number
  username: string
  role: string
  name: string
  email: string
  theme: string
  locale: string
}
export interface ServerStats {
  cpu_percent: number
  mem_used: number
  mem_total: number
  swap_used: number
  swap_total: number
  disk_used: number
  disk_total: number
}
export interface App {
  domain: string
  command: string
  port: number
  autostart: boolean
  state: string
  pid: number
  restarts: number
  repo_url?: string
  branch?: string
  webhook_path?: string
  webhook_secret?: string
  owner_type?: string
  owner_id?: number
}
export interface EmailSettings {
  backend: 'off' | 'smtp' | 'lamunmail'
  host?: string
  port?: number
  username?: string
  password?: string
  from?: string
  tls?: boolean
  api_base?: string
  api_key?: string
}

// deployArchive mengunggah .zip ke endpoint deploy (multipart, bukan JSON).
export async function deployArchive(domain: string, file: File): Promise<{ bytes: number }> {
  const form = new FormData()
  form.append('archive', file)
  const headers: Record<string, string> = {}
  const tok = getToken()
  if (tok) headers['Authorization'] = `Bearer ${tok}`
  const res = await fetch(`/api/v1/sites/${encodeURIComponent(domain)}/deploy`, {
    method: 'POST',
    headers,
    body: form,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : {}
  if (!res.ok) throw new ApiError(res.status, data.error || `HTTP ${res.status}`)
  return data
}

// Respons login: normal (token+role) atau butuh langkah kedua MFA.
export type LoginResult =
  | { token: string; role: string }
  | { mfa_required: true; pending: string }

export const api = {
  login: (username: string, password: string) =>
    req<LoginResult>('POST', '/auth/login', { username, password }),
  loginMFA: (pending: string, code: string) =>
    req<{ token: string; role: string }>('POST', '/auth/login/mfa', { pending, code }),

  // MFA (TOTP) milik pengguna sendiri.
  mfaStatus: () => req<{ enabled: boolean }>('GET', '/account/mfa'),
  mfaSetup: () => req<{ secret: string; uri: string }>('POST', '/account/mfa/setup'),
  mfaVerify: (code: string) => req<{ recovery_codes: string[] }>('POST', '/account/mfa/verify', { code }),
  mfaDisable: (code: string) => req<void>('POST', '/account/mfa/disable', { code }),
  me: () => req<Me>('GET', '/auth/me'),
  changePassword: (current: string, next: string) =>
    req<{ status: string }>('POST', '/auth/password', { current, new: next }),

  // R3: setup wizard + profil/preferensi.
  setupStatus: () => req<{ needs_setup: boolean }>('GET', '/setup/status'),
  setup: (b: { username: string; password: string; name?: string; email?: string; locale?: string; theme?: string }) =>
    req<{ token: string; role: string }>('POST', '/setup', b),
  updateAccount: (b: { name: string; email: string }) => req<{ name: string; email: string }>('PATCH', '/account', b),
  setPrefs: (b: { theme: string; locale: string }) => req<{ theme: string; locale: string }>('PUT', '/account/prefs', b),

  listSites: () => req<{ sites: Site[] }>('GET', '/sites').then((r) => r.sites),
  getSite: (domain: string) => req<Site>('GET', `/sites/${encodeURIComponent(domain)}`),
  createSite: (s: Partial<Site>) => req<Site>('POST', '/sites', s),
  patchSite: (domain: string, patch: Partial<Site>) =>
    req<Site>('PATCH', `/sites/${encodeURIComponent(domain)}`, patch),
  deleteSite: (domain: string) =>
    req<{ status: string }>('DELETE', `/sites/${encodeURIComponent(domain)}`),
  siteCert: (domain: string) => req<Cert>('GET', `/sites/${encodeURIComponent(domain)}/cert`),
  siteFiles: (domain: string) =>
    req<{ files: SiteFile[] }>('GET', `/sites/${encodeURIComponent(domain)}/files`).then((r) => r.files),
  siteReadFile: (domain: string, path: string) =>
    req<{ path: string; content: string }>('GET', `/sites/${encodeURIComponent(domain)}/file?path=${encodeURIComponent(path)}`),
  siteWriteFile: (domain: string, path: string, content: string) =>
    req<{ status: string }>('PUT', `/sites/${encodeURIComponent(domain)}/file`, { path, content }),
  siteMkdir: (domain: string, path: string) =>
    req<{ status: string }>('POST', `/sites/${encodeURIComponent(domain)}/folder`, { path }),
  siteDeleteFile: (domain: string, path: string) =>
    req<{ status: string }>('DELETE', `/sites/${encodeURIComponent(domain)}/file?path=${encodeURIComponent(path)}`),
  deploy: deployArchive,
  getRoutes: (domain: string) =>
    req<{ routes: RouteRule[] }>('GET', `/sites/${encodeURIComponent(domain)}/routes`).then((r) => r.routes),
  putRoutes: (domain: string, routes: RouteRule[]) =>
    req<{ routes: RouteRule[] }>('PUT', `/sites/${encodeURIComponent(domain)}/routes`, { routes }),
  siteLogs: (domain: string, n = 100) =>
    req<{ lines: string[] }>('GET', `/sites/${encodeURIComponent(domain)}/logs?n=${n}`).then((r) => r.lines),

  listCerts: () => req<{ certs: Cert[] }>('GET', '/certs').then((r) => r.certs),

  serverStats: () => req<ServerStats>('GET', '/server/stats'),

  // R7: observability.
  analyticsOverview: () => req<AnalyticsOverview>('GET', '/analytics/overview'),
  siteAnalytics: (domain: string) => req<DomainReport>('GET', `/sites/${encodeURIComponent(domain)}/analytics`),

  listApps: () => req<{ apps: App[] }>('GET', '/apps').then((r) => r.apps),
  createApp: (a: {
    domain: string
    command: string
    autostart: boolean
    dns_auto?: boolean
    repo_url?: string
    branch?: string
    owner_type?: string
    owner_id?: number
  }) => req<App>('POST', '/apps', a),
  deployGit: (domain: string) => req<{ status: string }>('POST', `/apps/${encodeURIComponent(domain)}/deploy-git`),
  siteDeployGit: (domain: string) => req<{ status: string }>('POST', `/sites/${encodeURIComponent(domain)}/deploy-git`),
  siteDeployLog: (domain: string) => req<{ status: string; lines: string[] }>('GET', `/sites/${encodeURIComponent(domain)}/deploy-log`),
  siteDeploys: (domain: string) => req<{ deploys: { id: number; status: string; trigger: string; commit: string; message: string; started_at: string; finished_at: string }[] }>('GET', `/sites/${encodeURIComponent(domain)}/deploys`),
  siteConnectGit: (domain: string, body: { repo_url: string; branch: string; build_cmd: string; output_dir: string }) => req<Site>('POST', `/sites/${encodeURIComponent(domain)}/connect-git`, body),
  siteDisconnectGit: (domain: string) => req<Site>('POST', `/sites/${encodeURIComponent(domain)}/disconnect-git`),
  siteCreateRepo: (domain: string, body: { name: string; private: boolean; branch: string; build_cmd: string; output_dir: string }) => req<Site>('POST', `/sites/${encodeURIComponent(domain)}/create-repo`, body),
  siteDomainStatus: (domain: string) => req<{ public_ip: string; domains: { domain: string; primary: boolean; points_here: boolean; resolved: string[]; error?: string }[] }>('GET', `/sites/${encodeURIComponent(domain)}/domains/status`),
  siteWebhook: (domain: string) => req<{ webhook_path: string; webhook_secret: string }>('GET', `/sites/${encodeURIComponent(domain)}/webhook`),
  siteWebhookRegen: (domain: string) => req<{ webhook_path: string; webhook_secret: string }>('POST', `/sites/${encodeURIComponent(domain)}/webhook/regenerate`),
  siteDomains: (domain: string) => req<{ domains: string[] }>('GET', `/sites/${encodeURIComponent(domain)}/domains`),
  addSiteDomain: (domain: string, alias: string) => req<{ domains: string[] }>('POST', `/sites/${encodeURIComponent(domain)}/domains`, { domain: alias }),
  removeSiteDomain: (domain: string, alias: string) => req<{ domains: string[] }>('DELETE', `/sites/${encodeURIComponent(domain)}/domains/${encodeURIComponent(alias)}`),
  getEnv: (domain: string) =>
    req<{ env: Record<string, string> }>('GET', `/apps/${encodeURIComponent(domain)}/env`).then((r) => r.env),
  putEnv: (domain: string, env: Record<string, string>) =>
    req<{ env: Record<string, string> }>('PUT', `/apps/${encodeURIComponent(domain)}/env`, { env }),
  getApp: (domain: string) => req<App>('GET', `/apps/${encodeURIComponent(domain)}`),
  deleteApp: (domain: string) => req<{ status: string }>('DELETE', `/apps/${encodeURIComponent(domain)}`),
  startApp: (domain: string) => req<App>('POST', `/apps/${encodeURIComponent(domain)}/start`),
  stopApp: (domain: string) => req<App>('POST', `/apps/${encodeURIComponent(domain)}/stop`),
  restartApp: (domain: string) => req<App>('POST', `/apps/${encodeURIComponent(domain)}/restart`),
  appLogs: (domain: string, n = 100) =>
    req<{ lines: string[] }>('GET', `/apps/${encodeURIComponent(domain)}/logs?n=${n}`).then((r) => r.lines),
  buildApp: (domain: string) => req<{ status: string }>('POST', `/apps/${encodeURIComponent(domain)}/build`),
  buildStatus: (domain: string) =>
    req<{ status: string; message: string; type: string }>('GET', `/apps/${encodeURIComponent(domain)}/build`),
  buildLogs: (domain: string) =>
    req<{ lines: string[] }>('GET', `/apps/${encodeURIComponent(domain)}/build/logs`).then((r) => r.lines),

  // R4: teams & membership.
  listTeams: () => req<{ teams: Team[] }>('GET', '/teams').then((r) => r.teams),
  createTeam: (name: string, slug?: string) => req<Team>('POST', '/teams', { name, slug }),
  getTeam: (id: number) => req<{ id: number; name: string; slug: string; members: TeamMember[] }>('GET', `/teams/${id}`),
  deleteTeam: (id: number) => req<{ status: string }>('DELETE', `/teams/${id}`),
  addMember: (id: number, username: string, role: string) =>
    req<TeamMember>('POST', `/teams/${id}/members`, { username, role }),
  removeMember: (id: number, userId: number) => req<{ status: string }>('DELETE', `/teams/${id}/members/${userId}`),
  createInviteLink: (id: number, role: string) =>
    req<{ token: string; path: string }>('POST', `/teams/${id}/invite-link`, { role }),
  acceptInvite: (token: string) => req<{ status: string }>('POST', `/teams/invites/${token}/accept`),
  searchUsers: (q: string) =>
    req<{ users: { username: string; name: string }[] }>('GET', `/users/search?q=${encodeURIComponent(q)}`).then((r) => r.users),

  // R8: connected accounts.
  listConnections: () => req<{ connections: Connection[]; github_device: boolean }>('GET', '/connections'),
  setConnection: (provider: string, token: string) => req<Connection>('PUT', `/connections/${provider}`, { token }),
  deleteConnection: (provider: string) => req<{ status: string }>('DELETE', `/connections/${provider}`),
  githubRepos: () =>
    req<{ repos: { full_name: string; private: boolean; clone_url: string }[] }>('GET', '/connections/github/repos').then((r) => r.repos),
  githubBranches: (owner: string, repo: string) =>
    req<{ branches: { name: string }[] }>('GET', `/connections/github/branches?owner=${encodeURIComponent(owner)}&repo=${encodeURIComponent(repo)}`),
  githubDeviceStart: () =>
    req<{ user_code: string; verification_uri: string; interval: number }>('POST', '/connections/github/device/start'),
  githubDevicePoll: () =>
    req<{ status: 'pending' | 'connected' | 'error'; login?: string; error?: string }>('POST', '/connections/github/device/poll'),

  listUsers: () => req<{ users: User[] }>('GET', '/users').then((r) => r.users),
  createUser: (u: { username: string; password: string; role: string; max_sites: number }) =>
    req<User>('POST', '/users', u),
  deleteUser: (id: number) => req('DELETE', `/users/${id}`),
  setQuota: (id: number, q: { max_sites: number; max_storage_mb: number; max_bandwidth_gb: number; max_teams: number; max_memory_mb: number; max_cpu_percent: number; max_apps: number }) =>
    req('PATCH', `/users/${id}/quota`, q),
  setUserStatus: (id: number, disabled: boolean) =>
    req('PATCH', `/users/${id}/status`, { disabled }),
  adminResetMfa: (id: number) => req('POST', `/admin/users/${id}/mfa/reset`),

  listKeys: () => req<{ apikeys: ApiKey[] }>('GET', '/apikeys').then((r) => r.apikeys),
  createKey: (name: string) => req<{ id: number; name: string; key: string }>('POST', '/apikeys', { name }),
  deleteKey: (id: number) => req('DELETE', `/apikeys/${id}`),

  // Tahap 3: setelan email (superadmin).
  getEmailSettings: () => req<EmailSettings>('GET', '/email/settings'),
  putEmailSettings: (body: EmailSettings) => req<{ status: string }>('PUT', '/email/settings', body),
  testEmail: () => req<{ status: string }>('POST', '/email/test'),

  // Tahap 3: reset kata sandi (public — req() tanpa token sudah aman).
  forgotPassword: (email: string) => req<{ status: string }>('POST', '/auth/forgot', { email }),
  resetPassword: (token: string, next: string) => req<{ status: string }>('POST', '/auth/reset', { token, new: next }),
  verifyEmail: (token: string) => req<{ status: string }>('POST', '/auth/verify', { token }),

  // Tahap 3: undang via email.
  inviteEmail: (teamId: number, email: string, role: string) =>
    req<{ status: string }>('POST', `/teams/${teamId}/invite-email`, { email, role }),

  // DNS zone manager.
  dnsZones: () =>
    req<{ zones: { domain: string; owner_type: string; owner_id: number; record_count: number; serial: number }[] }>('GET', '/dns/zones'),
  createDnsZone: (domain: string) => req('POST', '/dns/zones', { domain }),
  dnsZone: (d: string) =>
    req<{ zone: Record<string, unknown>; records: DnsRecord[]; nameservers: string[]; glue: { host: string; ip: string }[] }>('GET', `/dns/zones/${encodeURIComponent(d)}`),
  deleteDnsZone: (d: string) => req('DELETE', `/dns/zones/${encodeURIComponent(d)}`),
  addDnsRecord: (d: string, r: Partial<DnsRecord>) =>
    req<{ records: DnsRecord[] }>('POST', `/dns/zones/${encodeURIComponent(d)}/records`, r),
  patchDnsRecord: (d: string, id: number, r: Partial<DnsRecord>) =>
    req<{ records: DnsRecord[] }>('PATCH', `/dns/zones/${encodeURIComponent(d)}/records/${id}`, r),
  deleteDnsRecord: (d: string, id: number) =>
    req<{ records: DnsRecord[] }>('DELETE', `/dns/zones/${encodeURIComponent(d)}/records/${id}`),
  dnsSettings: () =>
    req<{ ns1: string; ns2: string; hostmaster: string; public_ip: string }>('GET', '/dns/settings'),
  setDnsSettings: (s: { ns1: string; ns2: string; hostmaster: string }) =>
    req('PUT', '/dns/settings', s),
  dnsZoneFor: (domain: string) =>
    req<{ managed: boolean; zone: string; label: string; public_ip: string }>('GET', `/dns/zone-for?domain=${encodeURIComponent(domain)}`),
}
