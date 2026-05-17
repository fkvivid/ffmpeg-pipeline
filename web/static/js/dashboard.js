import { escapeHtml, formatDate, formatVMAF } from './util.js'

const dropzone = document.getElementById('dropzone')
const fileInput = document.getElementById('fileInput')
const progressPanel = document.getElementById('progressPanel')
const statusText = document.getElementById('statusText')
const uploadBar = document.getElementById('uploadBar')
const renditionsDiv = document.getElementById('renditions')
const historyBody = document.getElementById('historyBody')
const historyTable = document.getElementById('historyTable')
const historyEmpty = document.getElementById('historyEmpty')

dropzone.addEventListener('click', () => fileInput.click())
fileInput.addEventListener('change', () => {
  if (fileInput.files[0]) upload(fileInput.files[0])
})

dropzone.addEventListener('dragover', (e) => {
  e.preventDefault()
  dropzone.classList.add('drag')
})
dropzone.addEventListener('dragleave', () => dropzone.classList.remove('drag'))
dropzone.addEventListener('drop', (e) => {
  e.preventDefault()
  dropzone.classList.remove('drag')
  if (e.dataTransfer.files[0]) upload(e.dataTransfer.files[0])
})

loadJobHistory()

async function loadJobHistory() {
  try {
    const res = await fetch('/api/jobs')
    if (!res.ok) return
    renderJobHistory(await res.json())
  } catch (e) {
    console.warn('Could not load job history', e)
  }
}

function renderJobHistory(jobs) {
  if (!jobs.length) {
    historyTable.classList.add('hidden')
    historyEmpty.classList.remove('hidden')
    return
  }
  historyEmpty.classList.add('hidden')
  historyTable.classList.remove('hidden')
  historyBody.innerHTML = jobs
    .map((job) => {
      const watchBtn =
        job.status === 'done'
          ? `<a class="btn btn-primary" href="/player.html?job=${encodeURIComponent(job.id)}">Watch</a>`
          : ''
      return `
        <tr data-job-id="${escapeHtml(job.id)}">
          <td class="col-filename" title="${escapeHtml(job.filename)}">${escapeHtml(job.filename)}</td>
          <td><span class="badge badge-${escapeHtml(job.status)}">${escapeHtml(job.status)}</span></td>
          <td class="vmaf-cell">${formatVMAF(job)}</td>
          <td class="col-date">${formatDate(job.created_at)}</td>
          <td class="actions">
            ${watchBtn}
            <button class="btn btn-ghost" type="button" data-delete="${escapeHtml(job.id)}">Delete</button>
          </td>
        </tr>
      `
    })
    .join('')

  historyBody.querySelectorAll('[data-delete]').forEach((btn) => {
    btn.addEventListener('click', () => deleteJob(btn.dataset.delete))
  })
}

async function deleteJob(id) {
  if (!confirm('Delete this job and all encoded files?')) return
  const res = await fetch(`/api/jobs/${id}`, { method: 'DELETE' })
  if (res.ok) loadJobHistory()
  else alert('Could not delete job')
}

function upload(file) {
  const form = new FormData()
  form.append('video', file)

  progressPanel.classList.add('visible')
  renditionsDiv.innerHTML = ''
  statusText.textContent = `Uploading ${file.name}…`
  uploadBar.style.width = '0%'
  uploadBar.classList.remove('done')

  const xhr = new XMLHttpRequest()
  xhr.open('POST', '/api/upload')

  xhr.upload.onprogress = (e) => {
    if (e.lengthComputable) {
      uploadBar.style.width = `${(e.loaded / e.total) * 100}%`
    }
  }

  xhr.onload = () => {
    if (xhr.status === 200) {
      const data = JSON.parse(xhr.responseText)
      statusText.innerHTML = `<strong>${escapeHtml(data.info.filename)}</strong> · <code>${escapeHtml(data.job_id)}</code>`
      uploadBar.style.width = '100%'
      uploadBar.classList.add('done')

      renditionsDiv.innerHTML = data.renditions
        .map(
          (name) => `
        <div class="rendition" data-rendition="${escapeHtml(name)}">
          <div class="rendition-header">
            <span class="rendition-name">${escapeHtml(name)}</span>
            <span class="rendition-pct">0%</span>
          </div>
          <div class="bar"><div class="bar-fill"></div></div>
        </div>
      `
        )
        .join('')

      listenForProgress(data.job_id)
      loadJobHistory()
    } else {
      statusText.textContent = `Failed: ${xhr.responseText}`
    }
  }

  xhr.onerror = () => {
    statusText.textContent = 'Network error'
  }
  xhr.send(form)
}

function listenForProgress(jobID) {
  const es = new EventSource(`/api/events/${jobID}`)

  es.onmessage = (event) => {
    const data = JSON.parse(event.data)

    if (data.event === 'progress') {
      const row = document.querySelector(`[data-rendition="${data.rendition}"]`)
      if (!row) return
      const bar = row.querySelector('.bar-fill')
      const pct = row.querySelector('.rendition-pct')
      bar.style.width = `${data.percent}%`
      pct.textContent = `${data.percent.toFixed(0)}%`
      if (data.done) {
        bar.classList.add('done')
        pct.classList.add('done')
        pct.textContent = 'Done'
      }
    }

    if (data.event === 'status' || data.event === 'vmaf_score') {
      loadJobHistory()
    }

    if (data.event === 'done') {
      statusText.innerHTML += ` <span style="color:var(--success)">Complete</span>`
      renditionsDiv.insertAdjacentHTML(
        'beforeend',
        `<a class="btn btn-primary" style="margin-top:16px" href="/player.html?job=${encodeURIComponent(jobID)}">Watch video</a>`
      )
      es.close()
      loadJobHistory()
    }

    if (data.event === 'error') {
      statusText.innerHTML += ` <span style="color:var(--danger)">${escapeHtml(data.error)}</span>`
      es.close()
      loadJobHistory()
    }
  }

  es.onerror = () => console.warn('SSE connection lost')
}
