import { qualityColor, qualityLabel } from './util.js'

const params = new URLSearchParams(location.search)
const jobID = params.get('job')

let hlsInstance = null

if (!jobID) {
  document.querySelector('.app-main').innerHTML =
    '<p class="error-page">No job ID. Use <code>?job=job_xxxxx</code></p>'
} else {
  init()
}

async function init() {
  const video = document.getElementById('video')
  const qualitySelect = document.getElementById('qualitySelect')
  const bandwidthSelect = document.getElementById('bandwidthSelect')
  const currentLevel = document.getElementById('currentLevel')
  const titleEl = document.getElementById('pageTitle')
  const infoPanel = document.getElementById('infoPanel')
  const infoContent = document.getElementById('infoContent')

  try {
    const res = await fetch(`/api/jobs/${jobID}`)
    if (!res.ok) throw new Error('Job not found')
    const job = await res.json()

    titleEl.textContent = job.filename
    document.title = `${job.filename} · ffmpeg-pipeline`
    initPlayer(video, qualitySelect, bandwidthSelect, currentLevel, infoPanel, infoContent)

    if (job.vmaf_scores?.length) {
      renderVMAF(job.vmaf_scores)
    } else if (job.status !== 'done' && job.status !== 'failed') {
      listenForVMAF(job)
    }
  } catch (err) {
    document.querySelector('.app-main').innerHTML =
      `<p class="error-page">${err.message}</p>`
  }
}

/** Highest level index whose bitrate fits under the simulated cap (with headroom). */
function capLevelForBandwidth(levels, bps) {
  const budget = bps * 0.9
  let cap = 0
  for (let i = 0; i < levels.length; i++) {
    if (levels[i].bitrate <= budget) {
      cap = i
    }
  }
  return cap
}

function applySimulatedBandwidth(hls, bps, qualitySelect) {
  if (!hls?.levels?.length) return

  if (bps <= 0) {
    hls.autoLevelCapping = -1
    hls.config.abrEwmaDefaultEstimate = 5e6
  } else {
    hls.config.abrEwmaDefaultEstimate = bps
    hls.autoLevelCapping = capLevelForBandwidth(hls.levels, bps)
  }

  qualitySelect.value = '-1'
  hls.currentLevel = -1
}

function formatBandwidthLabel(bps) {
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  return `${Math.round(bps / 1000)} kbps`
}

function renderVMAF(scores) {
  const panel = document.getElementById('vmafPanel')
  const content = document.getElementById('vmafContent')
  panel.classList.remove('hidden')

  const heightOf = (name) => parseInt(name, 10)
  scores.sort((a, b) => heightOf(b.rendition) - heightOf(a.rendition))

  content.innerHTML = scores
    .map((s) => {
      const color = qualityColor(s.mean)
      const label = qualityLabel(s.mean)
      return `
        <div class="vmaf-row" data-rendition="${s.rendition}">
          <span class="vmaf-name">${s.rendition}</span>
          <div class="vmaf-track">
            <div class="vmaf-fill" style="width:${s.mean}%;background:${color}"></div>
          </div>
          <span class="vmaf-score" style="color:${color}">${s.mean.toFixed(1)}</span>
          <span class="vmaf-label">${label}</span>
        </div>
      `
    })
    .join('')
}

function listenForVMAF(job) {
  const panel = document.getElementById('vmafPanel')
  const content = document.getElementById('vmafContent')
  panel.classList.remove('hidden')

  content.innerHTML = job.renditions
    .map(
      (name) => `
    <div class="vmaf-row" data-rendition="${name}">
      <span class="vmaf-name">${name}</span>
      <div class="vmaf-track"><div class="vmaf-fill" style="width:0%"></div></div>
      <span class="vmaf-score vmaf-pending">…</span>
      <span class="vmaf-label vmaf-pending">scoring</span>
    </div>
  `
    )
    .join('')

  const es = new EventSource(`/api/events/${jobID}`)
  es.onmessage = (e) => {
    const data = JSON.parse(e.data)

    if (data.event === 'vmaf_start') {
      const row = content.querySelector(`[data-rendition="${data.rendition}"]`)
      if (row) row.querySelector('.vmaf-label').textContent = 'scoring…'
    }

    if (data.event === 'vmaf_score') {
      const row = content.querySelector(`[data-rendition="${data.rendition}"]`)
      if (!row) return
      const color = qualityColor(data.mean)
      row.querySelector('.vmaf-fill').style.width = `${data.mean}%`
      row.querySelector('.vmaf-fill').style.background = color

      const scoreEl = row.querySelector('.vmaf-score')
      scoreEl.textContent = data.mean.toFixed(1)
      scoreEl.style.color = color
      scoreEl.classList.remove('vmaf-pending')

      const labelEl = row.querySelector('.vmaf-label')
      labelEl.textContent = qualityLabel(data.mean)
      labelEl.classList.remove('vmaf-pending')
    }

    if (data.event === 'done' || data.event === 'error') {
      es.close()
    }
  }
}

function tryAutoplay(video) {
  video.play().catch(() => {})
}

function updateLevelLabel(hls, currentLevel, bandwidthSelect) {
  const level = hls.levels[hls.currentLevel]
  if (!level) return

  const simBps = parseInt(bandwidthSelect.value, 10)
  const est = hls.bandwidthEstimate
  let extra = ''
  if (simBps > 0) {
    extra = ` · cap ${formatBandwidthLabel(simBps)}`
  } else if (est > 0) {
    extra = ` · est ${formatBandwidthLabel(est)}`
  }
  currentLevel.textContent = `Now playing ${level.height}p (${(level.bitrate / 1000).toFixed(0)} kbps)${extra}`
}

function initPlayer(video, qualitySelect, bandwidthSelect, currentLevel, infoPanel, infoContent) {
  const src = `/stream/${jobID}/master.m3u8`

  if (typeof Hls !== 'undefined' && Hls.isSupported()) {
    const hls = new Hls({
      startLevel: -1,
      enableWorker: true,
      abrEwmaDefaultEstimate: 5e6,
    })
    hlsInstance = hls
    hls.loadSource(src)
    hls.attachMedia(video)

    hls.on(Hls.Events.MANIFEST_PARSED, (_, data) => {
      tryAutoplay(video)
      qualitySelect.innerHTML = '<option value="-1">Auto (ABR)</option>'
      data.levels.forEach((level, i) => {
        const opt = document.createElement('option')
        opt.value = i
        opt.textContent = `${level.height}p — ${(level.bitrate / 1000).toFixed(0)} kbps`
        qualitySelect.appendChild(opt)
      })

      infoPanel.classList.remove('hidden')
      infoContent.innerHTML = data.levels
        .map(
          (l) => `
        <div class="info-row">
          <span>${l.width}×${l.height}</span>
          <strong>${(l.bitrate / 1000).toFixed(0)} kbps</strong>
        </div>
      `
        )
        .join('')

      applySimulatedBandwidth(hls, parseInt(bandwidthSelect.value, 10), qualitySelect)
    })

    hls.on(Hls.Events.LEVEL_SWITCHED, () => {
      updateLevelLabel(hls, currentLevel, bandwidthSelect)
    })

    bandwidthSelect.addEventListener('change', () => {
      const bps = parseInt(bandwidthSelect.value, 10)
      applySimulatedBandwidth(hls, bps, qualitySelect)
      updateLevelLabel(hls, currentLevel, bandwidthSelect)
    })

    qualitySelect.addEventListener('change', () => {
      const v = parseInt(qualitySelect.value, 10)
      hls.currentLevel = v
      if (v >= 0) {
        bandwidthSelect.value = '0'
        hls.autoLevelCapping = -1
      }
    })

    hls.on(Hls.Events.ERROR, (_, data) => {
      if (data.fatal) console.error('HLS fatal error:', data)
    })
  } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
    video.src = src
    video.addEventListener('loadedmetadata', () => tryAutoplay(video), { once: true })
    qualitySelect.disabled = true
    bandwidthSelect.disabled = true
    qualitySelect.innerHTML = '<option>Native HLS (Safari)</option>'
    bandwidthSelect.title = 'Use Chrome/Firefox with hls.js, or Safari + Network Link Conditioner'
  } else {
    document.querySelector('.app-main').innerHTML =
      '<p class="error-page">HLS is not supported in this browser.</p>'
  }
}
