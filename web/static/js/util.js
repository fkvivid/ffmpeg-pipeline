/** Shared helpers for the ffmpeg-pipeline UI. */

export function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

export function formatDate(iso) {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleString(undefined, { dateStyle: 'short', timeStyle: 'short' })
}

export function qualityColor(score) {
  if (score >= 90) return '#22c55e'
  if (score >= 80) return '#22d3ee'
  if (score >= 70) return '#eab308'
  return '#ef4444'
}

export function qualityLabel(score) {
  if (score >= 90) return 'excellent'
  if (score >= 80) return 'good'
  if (score >= 70) return 'fair'
  return 'poor'
}

export function formatVMAF(job) {
  if (!job.vmaf_scores?.length) {
    if (job.status === 'scoring') return 'scoring…'
    return '—'
  }
  return job.vmaf_scores
    .map((s) => {
      const cls = s.mean >= 90 ? 'vmaf-good' : ''
      return `<span class="${cls}">${escapeHtml(s.rendition)} ${s.mean.toFixed(0)}</span>`
    })
    .join('')
}
