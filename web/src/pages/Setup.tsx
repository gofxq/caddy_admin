import {request,isStaticDemo} from '../request'
import { useEffect, useMemo, useState } from 'react'
import { Waypoints } from 'lucide-react'
import { SetupDNSGuidance, suggestedDNSAddress, dnsRecord } from '../components/SetupDNSGuidance'

import { HelpTooltip } from '../components/HelpTooltip'
import { NewPasswordField } from '../components/NewPasswordField'
import { DEFAULT_RESOLVER } from '../setup-defaults'
import { validNewPassword } from '../password'
import { CloudflareTokenHelp } from '../components/CloudflareTokenHelp'
import { SetupConfigurationSummary } from '../components/SetupConfigurationSummary'
import { ConfigurationFile } from '../components/ConfigurationFile'
import { setupConfiguration, type Configuration } from '../configuration'
import { checkSetupDNS, DOH_PRESETS, type DNSReport, type DoHProvider } from '../setup-dns'
import { SetupHandoff, type Handoff } from '../components/SetupHandoff'

type DNSPlan = { name: string; type: string; address: string; action: 'create' | 'update' | 'reuse'; old_address?: string; old_proxied?: boolean; warning?: string; fingerprint: string; inputKey: string }
type Form = { username: string; password: string; domain: string; resolvers: string }
type Check = { id: string; status: 'pass' | 'warning' | 'block'; message: string }
type Preflight = { normalized: { admin_domain: string; origin: string; admin_origin: string }; network_valid: boolean; network_error?: string; checks: Check[]; can_complete: boolean; requires_acknowledgement: boolean; warning_fingerprint: string }
const initial: Form = { username: 'admin', password: isStaticDemo?'demo-password':'', domain: isStaticDemo?'home.example.com':'', resolvers: DEFAULT_RESOLVER }
const defaultSteps = [{ id: 0, label: '管理员' }, { id: 1, label: '首个域名与 DNS' }, { id: 2, label: '确认' }]
const list = (value: string) => value.split(',').map(v => v.trim()).filter(Boolean)
const normalizeDomain = (value: string) => value.trim().toLowerCase().replace(/^\*\./, '').replace(/\.$/, '')
class SetupHTTPError extends Error { constructor(message: string, readonly code: string) { super(message) } }
async function json(response: Response) { const body = await response.json().catch(() => null); if (!response.ok) throw new SetupHTTPError(body?.error?.message ?? `请求失败（HTTP ${response.status}）`, body?.error?.code ?? ''); if (!body) throw new Error('响应格式无效，请核对服务状态后重试'); return body }

function domainError(value: string) {
    const domain = normalizeDomain(value)
    if (!domain) return '请填写 首个通配符域名。'
    if (domain.length + 11 > 253 || !domain.split('.').every(label => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))) return '域名不能包含协议、端口或路径；请填写有效域名，例如 h.example.com。'
    return ''
}

export function Setup({ pollLogin = true }: { pollLogin?: boolean }) {
    const [importFile, setImportFile] = useState<Configuration | null>(null), [importCandidate, setImportCandidate] = useState<Configuration | null>(null), [importName, setImportName] = useState(''), [confirmImport, setConfirmImport] = useState(false)
    const [step, setStep] = useState(0), [form, setForm] = useState(initial), [error, setError] = useState(''), [checking, setChecking] = useState(true), [externalCaddy, setExternalCaddy] = useState(false), [initialized, setInitialized] = useState(false)
    const [suggestions, setSuggestions] = useState<string[]>([]), [preflight, setPreflight] = useState<Preflight | null>(null), [ack, setAck] = useState(false), [submitting, setSubmitting] = useState(false)
    const [preflightBusy, setPreflightBusy] = useState(false)
    const [dnsAddress, setDNSAddress] = useState(isStaticDemo?'192.168.1.5':'')
    const [testTLS, setTestTLS] = useState(false), [token, setToken] = useState(isStaticDemo?'demo-token':''), [dnsPlan, setDNSPlan] = useState<DNSPlan | null>(null), [dnsConfirmed, setDNSConfirmed] = useState(false), [dnsBusy, setDNSBusy] = useState(false)
    const [credentialStatus, setCredentialStatus] = useState({ password: 'missing', token: 'missing', passwordError: '', tokenError: '', id: '' }), [usePassword, setUsePassword] = useState(false), [useToken, setUseToken] = useState(false)
    const tokenReady = credentialStatus.token === 'ready' ? useToken : credentialStatus.token === 'missing' && !!token
    const passwordReady = credentialStatus.password === 'ready' ? usePassword : credentialStatus.password === 'missing' && validNewPassword(form.password)
    const credentials = { setup_id: credentialStatus.id, use_configured_password: usePassword, use_configured_token: useToken }
    const tokenRequest = { ...(credentialStatus.token === 'missing' ? { token } : {}), use_configured_token: useToken, setup_id: credentialStatus.id }
    const autoDNS = !externalCaddy && !testTLS
    const [dohProvider, setDoHProvider] = useState<DoHProvider>('cloudflare')
    const dnsTarget = dnsRecord(dnsAddress)?.address ?? dnsAddress.trim()
    const dnsInputKey = JSON.stringify([normalizeDomain(form.domain), dnsTarget, token, useToken, credentialStatus.id])
    const dnsQueryKey = JSON.stringify([dnsInputKey, dohProvider])
    const [dnsVerifiedKey, setDNSVerifiedKey] = useState(''), [dnsAppliedKey, setDNSAppliedKey] = useState(''), [dnsReport, setDNSReport] = useState<DNSReport | null>(null), [dnsAction, setDNSAction] = useState<'preview' | 'confirm' | 'check' | null>(null)
    const dnsConfigured = !!dnsPlan && dnsPlan.inputKey === dnsInputKey && dnsConfirmed && dnsAppliedKey === dnsInputKey
    const dnsReady = dnsConfigured && dnsVerifiedKey === dnsQueryKey
    const httpsEntry = location.protocol === 'http:' ? `https://${location.hostname}` : location.origin
    useEffect(() => { setDNSPlan(null); setDNSConfirmed(false); setDNSAppliedKey('') }, [dnsInputKey])
    useEffect(() => { setDNSVerifiedKey(''); setDNSReport(null) }, [dnsQueryKey])
    const [recovering, setRecovering] = useState(false)
    const [adminOrigin, setAdminOrigin] = useState(''), [handoff, setHandoff] = useState<Handoff | null>(null), [handoffOffline, setHandoffOffline] = useState(false)
    const [connectionFailed, setConnectionFailed] = useState(false), [connectionAttempt, setConnectionAttempt] = useState(0), [handoffAttempt, setHandoffAttempt] = useState(0)
    useEffect(() => { let alive = true; setChecking(true); setConnectionFailed(false); void (async () => { try { const body = await json(await request('/api/v1/setup/status', { cache: 'no-store' })); if (alive) { setExternalCaddy(body.external_caddy === true); setTestTLS(body.test_tls === true); setDNSAddress(current => current || (body.external_caddy === true ? '' : suggestedDNSAddress(location.hostname))); setInitialized(body.initialized === true); if (body.initialized === true) setRecovering(true); setSuggestions(Array.isArray(body.resolver_suggestions) ? body.resolver_suggestions : []); setCredentialStatus({ password: body.admin_password_status ?? 'missing', token: body.cloudflare_token_status ?? 'missing', passwordError: body.admin_password_error ?? '', tokenError: body.cloudflare_token_error ?? '', id: body.setup_id ?? '' }); setUsePassword(false); setUseToken(false); setError('') } } catch { try { const state = await json(await request('/api/v1/setup/handoff', { cache: 'no-store' })) as Handoff; if (!state.initialized) throw new Error('setup status unavailable'); if (alive) { setHandoff(state); setExternalCaddy(state.mode === 'external'); setAdminOrigin(state.admin_origin); setError('') } } catch { if (alive) setConnectionFailed(true) } } finally { if (alive) setChecking(false) } })(); return () => { alive = false } }, [connectionAttempt])
    useEffect(() => {
        if ((!adminOrigin && !recovering) || !pollLogin || !isStaticDemo&&location.protocol === 'http:') return
        let alive = true, timer: number | undefined
        const restoreForm = () => { setRecovering(false); setAdminOrigin(''); setInitialized(false); setHandoff(null); setError('初始化尚未完成，已保留输入，请检查后重新提交。') }
        const poll = async () => {
            try {
                const value = await json(await request('/api/v1/setup/handoff', { cache: 'no-store' })) as Handoff
                if (alive) { if (value.initialized === false && value.manager_status === 'ready') { restoreForm(); return } if (value.initialized) { setHandoff(value); setAdminOrigin(value.admin_origin); setExternalCaddy(value.mode === 'external'); setHandoffOffline(false) } else { setHandoffOffline(true) } }
            } catch {
                if (alive) setHandoffOffline(true)
                if (recovering) { try { const value = await json(await request('/api/v1/setup/status', { cache: 'no-store' })); if (alive && value.initialized === false) { restoreForm(); return } } catch {/* Keep checking an unknown result without replaying setup. */ } }
            }
            if (alive) timer = window.setTimeout(() => void poll(), 3000)
        }
        void poll(); return () => { alive = false; window.clearTimeout(timer) }
    }, [adminOrigin, recovering, pollLogin, handoffAttempt])
    useEffect(() => { if (!isStaticDemo&&adminOrigin && location.protocol === 'http:' && pollLogin) location.assign(httpsEntry + '/setup') }, [adminOrigin, httpsEntry, pollLogin])
    const settings = useMemo(() => ({ domain: normalizeDomain(form.domain), resolvers: list(form.resolvers) }), [form])
    const importSettings = importFile ? { ...importFile.settings, domains: importFile.settings.domains, admin_domain: `caddyadmin.${normalizeDomain(form.domain)}`, origin: `https://caddyadmin.${normalizeDomain(form.domain)}`, resolvers: list(form.resolvers) } : undefined
    const field = (key: keyof Form) => (e: React.ChangeEvent<HTMLInputElement>) => { setForm({ ...form, [key]: e.target.value }); setPreflight(null); setAck(false); setConfirmImport(false) }
    const adminDomain = normalizeDomain(form.domain) ? `caddyadmin.${normalizeDomain(form.domain)}` : ''
    async function runPreflight() { setPreflightBusy(true); setError(''); try { const result = await json(await request('/api/v1/setup/preflight', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ settings, ...(importFile ? { services: importFile.services, import_settings: importSettings } : {}) }) })) as Preflight; if (preflight?.warning_fingerprint !== result.warning_fingerprint) setAck(false); setPreflight(result); return result } catch (e) { setError(e instanceof Error ? e.message : '预检失败，请重试'); return null } finally { setPreflightBusy(false) } }
    async function previewDNS() { setDNSBusy(true); setDNSAction('preview'); setDNSAppliedKey(''); setDNSVerifiedKey(''); setDNSReport(null); setError(''); setDNSConfirmed(false); setDNSPlan(null); const inputKey = dnsInputKey; try { const result = await json(await request('/api/v1/setup/dns/preview', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...tokenRequest, domain: normalizeDomain(form.domain), address: dnsTarget }) })); setDNSPlan({ ...result, inputKey }) } catch (e) { setError(e instanceof Error ? e.message : 'DNS 预览失败，请重试') } finally { setDNSBusy(false); setDNSAction(null) } }
    async function confirmDNS() { setDNSBusy(true); setDNSAction('confirm'); setError(''); setDNSAppliedKey(''); setDNSVerifiedKey(''); setDNSReport(null); const inputKey = dnsInputKey; try { await json(await request('/api/v1/setup/dns/confirm', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ settings, ...(importSettings ? { import_settings: importSettings } : {}), cloudflare: { ...tokenRequest, address: dnsTarget, fingerprint: dnsPlan!.fingerprint, confirmed: dnsConfirmed } }) })); setDNSAppliedKey(inputKey) } catch (e) { setError(e instanceof Error ? e.message : 'DNS 配置失败，请保留输入后重试'); if (e instanceof Error && e.message.includes('DNS 记录已变化')) { setDNSPlan(null); setDNSConfirmed(false) } } finally { setDNSBusy(false); setDNSAction(null) } }
    async function checkDNS() {
        setDNSBusy(true); setDNSAction('check'); setError(''); setDNSVerifiedKey(''); setDNSReport(null)
        const inputKey = dnsQueryKey
        try { const report = await checkSetupDNS(normalizeDomain(form.domain), dnsTarget, dohProvider); setDNSReport(report); if (report.verified) setDNSVerifiedKey(inputKey) }
        catch (e) { setError(e instanceof Error ? e.message : 'DNS 查询失败，请保留输入后重试') }
        finally { setDNSBusy(false); setDNSAction(null) }
    }
    async function next() { if (step === 0) { setStep(1); return } const result = await runPreflight(); if (result?.can_complete) setStep(2) }
    async function submit() {
        if (autoDNS && !dnsReady) { setError('请完成 DNS 配置确认和解析检查'); return }
        if (!preflight?.can_complete || preflight.requires_acknowledgement && !ack) { setError('请核对预检结果并确认警告'); return }
        setSubmitting(true); setError('')
        try {
            const data = await json(await request('/api/v1/setup/complete', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: form.username, ...(credentialStatus.password === 'missing' ? { password: form.password } : {}), ...credentials, settings, acknowledge_warnings: ack, warning_fingerprint: ack ? preflight.warning_fingerprint : '', ...(autoDNS ? { cloudflare: { ...tokenRequest, address: dnsTarget, fingerprint: dnsPlan!.fingerprint, confirmed: dnsConfirmed } } : {}), ...(importFile ? { services: importFile.services, confirm_import: confirmImport, import_settings: importSettings } : {}) }) }))
            if (typeof data.admin_origin !== 'string') throw new Error('初始化响应无法确认')
            setAdminOrigin(data.admin_origin); setToken(''); setForm({ ...form, password: '' })
        } catch (e) {
            if (e instanceof SetupHTTPError) {
                if (e.code === 'setup_warning_confirmation_required') {
                    setAck(false); const current = await runPreflight(); if (current) setError('预检警告已变化，请阅读当前警告并重新确认。')
                } else {
                    setError(e.message)
                    if (e.message.includes('DNS 记录已变化')) { setDNSPlan(null); setDNSConfirmed(false); setStep(1) }
                }
            } else { setRecovering(true); setHandoffOffline(true) }
        } finally { setSubmitting(false) }
    }
    if (adminOrigin || recovering) return <SetupHandoff adminOrigin={adminOrigin} handoff={handoff} offline={handoffOffline} externalCaddy={externalCaddy} httpsEntry={httpsEntry} testTLS={testTLS} onRetry={() => setHandoffAttempt(value => value + 1)} />
    if (checking) return <div className="setup-screen"><div className="setup-card"><h1>正在检查初始化状态</h1><p className="muted">请稍候…</p></div></div>
    if (connectionFailed) return <div className="setup-screen"><section className="setup-card"><h1>无法连接初始化服务</h1><p className="notice notice-amber" role="alert">暂时无法读取初始化状态，尚不能确认是否已初始化。请检查服务与网络后重试，已输入的信息会保留。</p><button className="button button-primary" onClick={() => setConnectionAttempt(value => value + 1)}>重试连接</button><p className="muted">若此前已提交初始化，请通过 <a href={httpsEntry + '/setup'}>HTTPS 交接页面</a> 核对结果，避免重复提交。</p></section></div>
    if (initialized) return <div className="setup-screen"><div className="setup-card"><h1>系统已经初始化</h1><p className="muted">请使用配置的控制台域名进入登录页面。</p></div></div>
    const steps = defaultSteps
    const blocks = preflight?.checks.filter(check => check.status === 'block') ?? [], warnings = preflight?.checks.filter(check => check.status === 'warning') ?? []
    const domainFeedback = domainError(form.domain)
    const usernameFeedback = !form.username || new TextEncoder().encode(form.username).length > 64 ? '用户名必须为 1–64 字节。' : ''
    const validTarget = !!dnsRecord(dnsAddress)
    const serverDNSMissing = !list(form.resolvers).length
    const dnsNextAction = serverDNSMissing ? '请在「高级：服务器 DNS」填写证书校验使用的解析器。' : !tokenReady ? '请填写或确认使用 Cloudflare Token。' : !validTarget ? '请填写有效的 Caddy 访问 IP。' : !dnsPlan ? '下一步：预览 DNS 变更。' : !dnsConfigured ? '下一步：阅读变更影响，勾选确认后点击「确认配置」。' : !dnsReady ? '下一步：检查 DNS；失败后可直接重查或更换 DoH，无需再次写入记录。' : 'DNS 已配置且解析检查通过，可以继续。'
    const importedDrafts = importFile && <><h3>导入服务草稿（{importFile.services.length}）</h3>{importFile.services.length === 0 ? <p className="muted">文件中没有服务草稿，初始化后服务列表为空。</p> : <ul className="setup-drafts">{importFile.services.map((s, i) => <li key={i}>{s.name} · {s.hostname} · {s.enabled ? '启用' : '停用'}</li>)}</ul>}</>
    const domainFields = <div className="field-group"><div className="field-heading"><label htmlFor="setup-domain">首个通配符域名</label><HelpTooltip label="首个通配符域名说明">登记基础域名，控制台使用 caddyadmin 一层子域；业务访问范围登录后配置。</HelpTooltip></div><input id="setup-domain" placeholder="home.example.com" aria-invalid={!!form.domain && !!domainFeedback} aria-describedby="setup-domain-feedback" value={form.domain} onChange={field('domain')} /><small id="setup-domain-feedback" className={domainFeedback && form.domain ? 'text-danger' : 'muted'}>{domainFeedback || `控制台：https://${adminDomain}`}</small></div>
    const resolverField = <div className="field-group"><div className="field-heading"><label htmlFor="setup-resolvers">DNS 解析器（逗号分隔）</label><HelpTooltip label="DNS 解析器说明">服务器用于 Caddy 证书 DNS 校验及运行诊断的解析器，默认 1.1.1.1；填写 IP/端口。浏览器 DoH 查询独立选择，不改变这里的配置。</HelpTooltip></div><input id="setup-resolvers" required placeholder="例如：192.168.1.1" value={form.resolvers} onChange={field('resolvers')} /></div>
    return <div className="setup-screen"><section className={`setup-card ${step === 1 ? 'setup-dns-card' : ''}`}><div className="setup-brand"><Waypoints /><span>Caddy Admin</span></div><h1>初始化 Caddy Admin</h1><p className="muted">确认前不会修改 DNS；导入的服务仅保存为草稿。</p><div className="notice notice-amber">{isStaticDemo?'使用示例凭据即可体验初始化；DNS 与证书配置均在浏览器内模拟成功。':'第一个成功提交者将成为唯一管理员。请尽快完成初始化。'}</div>{!isStaticDemo&&location.protocol === 'http:' && <div className="notice notice-amber">HTTP 会明文传输手动填写的管理员密码和 Token，建议使用 <a href={httpsEntry + '/setup'}>HTTPS 初始化</a>。</div>}<ol className="setup-steps" aria-label="初始化步骤">{steps.map(({ id, label }, index) => <li key={id} aria-current={id === step ? 'step' : undefined} className={id === step ? 'active' : id < step ? 'done' : ''}><b>{index + 1}</b>{label}</li>)}</ol>{error && <div className="notice notice-red" role="alert">{error}{error.includes('预配置') && error.includes('失效') && <button className="button button-outline" onClick={() => { setPreflight(null); setAck(false); setConfirmImport(false); setStep(0); setConnectionAttempt(v => v + 1) }}>重新读取凭据状态</button>}</div>}<fieldset className="setup-form" disabled={preflightBusy || dnsBusy || submitting}>
        {step === 0 && <><details><summary>从导出的配置初始化</summary><p className="muted">导入域名、网络策略和服务草稿；管理员密码与 Cloudflare Token 不从文件导入。服务不会自动发布。</p><ConfigurationFile onSelect={() => setImportCandidate(null)} onLoad={(value, name) => { setImportCandidate(value); setImportName(name); setError('') }} />{importCandidate && <><p>{importName}：{importCandidate.settings.domains.map(d => d.name).join('、')}，{importCandidate.services.length} 个服务</p><button className="button button-outline" onClick={() => { try { const s = setupConfiguration(importCandidate); setForm({ ...form, domain: s.domain, resolvers: s.resolvers.join(',') }); setImportFile(importCandidate); setImportCandidate(null); setConfirmImport(false); setPreflight(null); setAck(false); setError('') } catch (e) { setError(e instanceof Error ? e.message : '无法采用该配置') } }}>采用导入配置</button></>}</details>{importFile && <p role="status" className="notice notice-green">已采用配置：{importFile.services.length} 个服务将保存为草稿。下一步集中核对设置，无需逐项重新填写。</p>}<h2>创建管理员</h2><label>管理员用户名<input aria-invalid={!!usernameFeedback} aria-describedby="setup-username-feedback" value={form.username} onChange={field('username')} autoComplete="username" /></label>{usernameFeedback && <p id="setup-username-feedback" className="text-danger">{usernameFeedback}</p>}{credentialStatus.password === 'ready' ? <><p>管理员密码已通过部署配置</p><label className="checkbox"><input type="checkbox" checked={usePassword} onChange={e => setUsePassword(e.target.checked)} />我确认使用已配置的管理员密码</label></> : credentialStatus.password === 'invalid' ? <p className="notice notice-red" role="alert">{credentialStatus.passwordError || '管理员密码部署配置有误，请修正或清空后重建容器。'}</p> : <NewPasswordField label="管理员密码" value={form.password} onChange={password => { setForm({ ...form, password }); setConfirmImport(false); setAck(false); setPreflight(null) }} />}</>}
        {step === 1 && <><div className="section-heading"><h2>首个域名与 DNS</h2><span className="muted">{externalCaddy ? '外部 Caddy' : testTLS ? '测试 TLS' : 'Cloudflare DNS-01'}</span></div>
            {domainFields}{importFile && <>{importedDrafts}<SetupConfigurationSummary settings={settings} importSettings={importSettings} adminDomain={adminDomain} />{importFile.settings.domains.length > 1 && <label>首个控制台域名<select value={setupConfiguration(importFile).domain} onChange={e => { const domain = e.target.value; setImportFile({ ...importFile, settings: { ...importFile.settings, admin_domain: `caddyadmin.${domain}`, origin: `https://caddyadmin.${domain}` } }); setForm({ ...form, domain }); setPreflight(null) }}>{importFile.settings.domains.map(d => <option key={d.id} value={d.name}>{d.name}</option>)}</select></label>}<button className="button button-outline" onClick={() => { setImportFile(null); setForm({ ...initial, username: form.username, password: form.password }); setConfirmImport(false); setPreflight(null); setError(dnsAppliedKey ? '取消导入不会撤销已写入的 DNS 记录，请重新核对。' : '') }}>取消导入</button></>}
            <div className="setup-dns-fields">
                <SetupDNSGuidance domain={normalizeDomain(form.domain)} address={dnsAddress} onAddressChange={value => { setDNSAddress(value); setPreflight(null); setAck(false) }} externalCaddy={externalCaddy} automatic={autoDNS} />
                <div className="setup-token">{autoDNS ? <>{credentialStatus.token === 'ready' ? <><p>Cloudflare Token 已通过部署配置</p><label className="checkbox"><input type="checkbox" checked={useToken} onChange={e => { setUseToken(e.target.checked); setPreflight(null); setAck(false) }} />我确认使用已配置的 Cloudflare Token</label></> : credentialStatus.token === 'invalid' ? <p className="notice notice-red" role="alert">{credentialStatus.tokenError || 'Cloudflare Token 部署配置有误，请修正或清空后重建容器。'}</p> : <label>Cloudflare API Token<input aria-label="Cloudflare API Token" type="password" autoComplete="off" value={token} onChange={e => { setToken(e.target.value); setPreflight(null); setAck(false) }} /></label>}<details><summary>获取 Token 与权限</summary><CloudflareTokenHelp /><p className="muted">{isStaticDemo?'使用任意演示 Token 即可，不会保存凭据或调用 Cloudflare。':'用于配置 DNS 和申请证书，仅保存到服务器受限 secret 文件。'}</p></details></> : <p className="notice notice-amber">{externalCaddy ? '证书与 DNS 凭据需在专用外部 Caddy 实例安全配置；本向导不读取或保存远端 Token。' : '当前 TEST_TLS=true，使用内部测试 CA，不使用 Cloudflare Token 或自动修改 DNS；不代表公网可信证书。'}</p>}</div>
                <div className="setup-resolvers"><div className="field-heading"><span>浏览器 DNS 查询（DoH）</span><HelpTooltip label="DoH 查询说明">点击检查时，浏览器向所选服务发送控制台和随机子域的 A/AAAA 查询，不发送密码、Token 或网络策略。通过只说明所选服务返回正确 IP，不代表设备可达或 HTTPS 就绪。</HelpTooltip></div><div className="dns-presets" aria-label="常用 DoH">{DOH_PRESETS.map(({ id, name }) => <button type="button" key={id} className="button button-outline button-sm" aria-pressed={dohProvider === id} onClick={() => setDoHProvider(id)}>{name}</button>)}</div><p className="muted">仅在点击「检查 DNS」时查询。可切换 DoH 重查，无需重复配置记录。</p><details><summary>高级：服务器 DNS</summary>{resolverField}{suggestions.length > 0 && <div><p className="muted">服务器 DNS 建议（不会自动采用）：</p>{suggestions.map(value => <button type="button" key={value} className="button button-outline button-sm" onClick={() => { setForm({ ...form, resolvers: value }); setPreflight(null); setAck(false); setConfirmImport(false) }}>采用 {value}</button>)}</div>}</details></div>
            </div>
            {autoDNS && dnsPlan && dnsPlan.inputKey === dnsInputKey && <div className="setup-dns-plan"><p role="status">{dnsPlan.action === 'create' ? '新建' : dnsPlan.action === 'update' ? '更新' : '复用'}：{dnsPlan.type} {dnsPlan.name} → {dnsPlan.address}，仅 DNS（灰云）。</p>{dnsPlan.action === 'update' && <p className="notice notice-amber">将覆盖旧地址 {dnsPlan.old_address}，旧代理状态：{dnsPlan.old_proxied ? '已代理' : '仅 DNS'}。</p>}{dnsPlan.warning && <p className="notice notice-amber">{dnsPlan.warning}</p>}<label className="checkbox warning"><input type="checkbox" checked={dnsConfirmed} onChange={e => { setDNSConfirmed(e.target.checked); setDNSVerifiedKey(''); setDNSReport(null) }} />我已确认上述 DNS 记录及变更影响。</label></div>}
            <div className="setup-dns-actions">{autoDNS && <><button type="button" className={`button ${!dnsPlan ? 'button-primary' : 'button-outline'}`} disabled={!tokenReady || !!domainFeedback || !validTarget || dnsBusy} onClick={() => void previewDNS()}>{dnsAction === 'preview' ? '正在预览 DNS…' : '预览 DNS 变更'}</button><button type="button" className={`button ${!dnsConfigured && dnsPlan ? 'button-primary' : 'button-outline'}`} disabled={!dnsPlan || dnsPlan.inputKey !== dnsInputKey || !dnsConfirmed || dnsConfigured || dnsBusy} onClick={() => void confirmDNS()}>{dnsAction === 'confirm' ? '正在确认配置…' : '确认配置'}</button></>}<button type="button" className={`button ${(!autoDNS || dnsConfigured) && !dnsReady ? 'button-primary' : 'button-outline'}`} disabled={!!dnsAddress.trim() && !validTarget || autoDNS && !validTarget || dnsBusy} onClick={() => void checkDNS()}>{dnsAction === 'check' ? '正在检查 DNS…' : '检查 DNS'}</button></div>
            <p className="muted">{autoDNS ? '预览不修改 DNS；确认配置立即写入 Cloudflare。检查 DNS 由浏览器通过 DoH 查询，不会再次写入；未生效时重查，不会自动撤销记录。初始化后申请证书，期间临时 HTTPS 入口可能显示信任提示。' : '请手动保存 DNS 记录后检查。查询由浏览器通过 DoH 执行，不证明设备可达或 HTTPS 已就绪。'}</p>
            {autoDNS && dnsAppliedKey === dnsInputKey && <p role="status" className="notice notice-green">{isStaticDemo?'DNS 配置已确认，已模拟成功；更换 DoH 后可继续模拟检查。':'DNS 配置已确认，已保存到 Cloudflare；更换 DoH 后只需重新检查解析。'}</p>}
            {(autoDNS || serverDNSMissing) && <p role="status" className="muted">{dnsNextAction}</p>}
            {dnsReport && <div className="setup-dns-results"><p role="status" className={`notice notice-${dnsReport.verified ? 'green' : 'amber'}`}>{dnsReport.verified ? 'DNS 查询通过。' : 'DNS 查询未通过，请核对记录、解析器或缓存后重查。'}</p><div className="table-wrap"><table aria-label="DNS 查询结果"><thead><tr><th>查询名称</th><th>解析器</th><th>返回地址</th><th>结果</th></tr></thead><tbody>{dnsReport.queries.map((query, index) => <tr key={`${query.name}-${query.resolver}-${index}`}><td><code>{query.name}</code></td><td><code>{query.resolver}</code></td><td>{query.addresses.length ? query.addresses.join('、') : '无地址'}</td><td className={query.status === 'pass' ? 'text-success' : 'text-danger'}>{query.message}</td></tr>)}</tbody></table></div></div>}
            {dnsReady && <p role="status" className="notice notice-green">DNS 已配置，控制台域名与通配符一层子域均解析到目标 IP。</p>}
            {blocks.map(check => <p className="notice notice-red" key={check.id}>{check.message}</p>)}
        </>}

        {step === 2 && <><h2>确认配置</h2>{blocks.map(check => <p className="notice notice-red" key={check.id}>{check.message}</p>)}<button type="button" className="button button-outline button-sm" disabled={preflightBusy || submitting} onClick={() => { setAck(false); void runPreflight() }}>重新预检</button>{autoDNS && dnsPlan && <p className="notice notice-amber">DNS 已配置并通过目标 IP 校验：{dnsPlan.name} → {dnsPlan.address}。完成初始化时由服务器核对 Cloudflare 记录与确认指纹；浏览器解析通过不代表服务器 DNS、证书或设备访问已就绪。</p>}{importFile && <>{importedDrafts}<label className="checkbox warning"><input type="checkbox" checked={confirmImport} onChange={e => setConfirmImport(e.target.checked)} />我已确认导入配置，服务仅保存为草稿。</label></>}<SetupConfigurationSummary settings={settings} importSettings={importSettings} adminDomain={preflight?.normalized.admin_domain || adminDomain} />{warnings.map(check => <div className="notice notice-amber" key={check.id}>{check.message}</div>)}{warnings.length > 0 && <label className="checkbox warning"><input type="checkbox" checked={ack} onChange={event => setAck(event.target.checked)} />我已理解上述警告，并确认继续初始化。</label>}<div className="notice notice-amber">{isStaticDemo?'完成后进入演示控制台，不会重启或修改真实服务。':'完成后服务会自动重启；关闭此页面不会取消后台操作。'}</div></>}
    </fieldset><div className="setup-actions">{step > 0 && <button className="button button-outline" disabled={preflightBusy || dnsBusy || submitting} onClick={() => setStep(steps[steps.findIndex(item => item.id === step) - 1].id)}>上一步</button>}<button className="button button-primary" onClick={() => step < 2 ? void next() : void submit()} disabled={submitting || preflightBusy || dnsBusy || (step === 0 && (!!usernameFeedback || !passwordReady)) || (step === 1 && (!!domainFeedback || !list(form.resolvers).length || autoDNS && !dnsReady)) || (step === 2 && (autoDNS && !dnsReady || !preflight?.can_complete || warnings.length > 0 && !ack || !!importFile && !confirmImport))}>{submitting ? '正在提交…' : preflightBusy ? '正在预检…' : step < 2 ? '下一步' : '完成初始化'}</button></div></section></div>
}
