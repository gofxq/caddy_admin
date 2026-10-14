import { Waypoints } from 'lucide-react'

export type AutomaticProgress = {
    status: 'manual' | 'running' | 'action_required' | 'failed' | 'uncertain' | 'saved'
    stage: string
    message?: string
    can_recheck?: boolean
    checks?: { id: string; status: 'pass' | 'info' | 'warning' | 'block'; message: string; next_step?: string }[]
}

const stages = [
    { id: 'configuration', label: '检查配置' },
    { id: 'doh', label: '检查 DoH 解析' },
    { id: 'dns', label: '核对凭据与 DNS' },
    { id: 'preflight', label: '服务器预检' },
    { id: 'saving', label: '保存初始化数据' },
    { id: 'restarting', label: '重启服务' },
    { id: 'handoff', label: '等待正式入口' },
]
const checkLabels: Record<string, string> = { setup_doh: '公共 DNS 解析（DoH）', settings_valid: '设置格式与安全边界', resolver_reachable: '服务器 DNS 可达性', admin_dns: '控制台域名解析' }

export function SetupProgress({ progress, offline, loading, rechecking, retryError, onManual, onReadStatus, onRecheck }: {
    progress: AutomaticProgress
    offline: boolean
    loading: boolean
    rechecking: boolean
    retryError: string
    onManual: () => void
    onReadStatus: () => void
    onRecheck: () => void
}) {
    const running = progress.status === 'running'
    const currentStage = stages.findIndex(stage => stage.id === progress.stage)
    const checksPassed = progress.status === 'action_required' && progress.stage === 'preflight' && !!progress.checks?.length && progress.checks.every(check => check.status === 'pass' || check.status === 'info')
    const title = running ? rechecking ? '正在重新检查 DNS' : '正在自动初始化' : checksPassed ? '检查已通过，等待确认' : progress.status === 'failed' ? '自动初始化未完成' : progress.status === 'uncertain' ? '初始化结果待核对' : '自动初始化需要处理'
    return <div className="setup-screen"><section className="setup-card">
        <div className="setup-brand"><Waypoints /><span>Caddy Admin</span></div>
        <h1>{title}</h1>
        <p className="muted">{rechecking ? '正在重新查询 DoH、读取 Cloudflare 记录并执行预检；不修改 DNS 或保存初始化数据。' : running ? '后台任务由服务启动触发，关闭页面不会取消。页面只读取进度。' : '自动流程已暂停，页面会保留当前状态。由你选择进入向导处理，不会自动提交或跳过确认。'}</p>
        <p className="muted">新实例默认不限制控制台来源，业务域名允许互联网访问，可信与上游网段覆盖全部 IPv4/IPv6，许可全部合法服务名。管理操作须登录；初始化仅建立控制台，业务服务须另行发布，之后服务导览会展示公开服务的名称和域名。</p>
        <ol className="setup-progress" aria-label="自动初始化进度">{stages.map((stage, index) => <li key={stage.id} aria-current={index === currentStage ? 'step' : undefined}>{stage.label}{index === currentStage ? running ? '（正在执行）' : '（需处理）' : index < currentStage ? '（已完成）' : '（尚未执行）'}</li>)}</ol>
        {progress.message && <p className={`notice notice-${progress.status === 'failed' ? 'red' : 'amber'}`} role="status">{progress.message}</p>}
        {!!progress.checks?.length && <section aria-label="预检详情"><h2>预检详情</h2><ul className="setup-progress-checks">{progress.checks.map(check => <li key={check.id}>
            <div><strong>{checkLabels[check.id] || '服务器检查'}</strong> · <span>{check.status === 'pass' ? '通过' : check.status === 'info' ? '提示，不阻断' : check.status === 'warning' ? '警告，需确认' : '阻断，需修复'}</span></div>
            <p className={`notice notice-${check.status === 'pass' ? 'green' : check.status === 'block' ? 'red' : 'amber'}`}>{check.message}</p>
            {check.next_step && <p className="muted">下一步：{check.next_step}</p>}
        </li>)}</ul><p className="muted">以上为最近一次检查结果；重新检查可核对当前情况。</p></section>}
        {retryError && <p className="notice notice-red" role="alert">{retryError}</p>}
        {offline && <p className="notice notice-amber" role="status">连接暂时中断，初始化结果待核对；读取状态不会重复提交。</p>}
        {(!running || rechecking) && <>
            <p className="muted">向导保留有效预配置项；DNS 变更和预检警告仍需阅读并明确确认。</p>
            <div className="setup-actions">
                <button className="button button-outline" disabled={loading || rechecking} onClick={onReadStatus}>重新读取状态</button>
                {(progress.can_recheck || rechecking) && <button className="button button-outline" disabled={loading || rechecking || offline} onClick={onRecheck}>{rechecking ? '正在重新检查…' : '重新检查 DNS 与预检'}</button>}
                <button className="button button-primary" disabled={loading || rechecking || offline} onClick={onManual}>进入向导处理</button>
            </div>
        </>}
    </section></div>
}
