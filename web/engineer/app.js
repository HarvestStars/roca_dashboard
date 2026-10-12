// Rocateq field engineer PWA. Plain JS, no build step.
(() => {
  const $ = (id) => document.getElementById(id);
  const params = new URLSearchParams(location.search);
  const state = { engineer: params.get("engineer_id") || "", tripId: params.get("trip_id") || "", detail: null, editing: new Set(), samplesLoaded: false };
  const CATS = ["HOTEL", "TRANSPORT_FLIGHT_TRAIN", "TAXI", "MEAL", "TOOLS_SUPPLIES", "OTHER"];
  const STATUS_TEXT = {
    ASSIGNED: "Assigned", ON_SITE: "On site", WORK_DONE: "Work done", SUBMITTED: "Submitted", APPROVED: "Approved",
    ANALYZING: "Reading…", EXTRACTED: "Read by AI", NEEDS_REVIEW: "Please check", CONFIRMED: "Confirmed", FAILED: "Enter manually",
  };

  const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  const eur = (n) => "€" + Number(n || 0).toFixed(2);
  const time = (iso) => new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  const dateTime = (iso) => new Date(iso).toLocaleString([], { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });

  async function api(path, opts = {}) {
    const r = await fetch(path, opts);
    const body = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(body.error || r.statusText);
    return body;
  }
  const post = (path, data) => api(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(data || {}) });

  function toast(msg) {
    const t = $("toast");
    t.textContent = msg;
    t.classList.remove("hidden");
    clearTimeout(toast.timer);
    toast.timer = setTimeout(() => t.classList.add("hidden"), 3500);
  }

  function syncUrl() {
    const p = new URLSearchParams({ engineer_id: state.engineer });
    if (state.tripId) p.set("trip_id", state.tripId);
    history.replaceState(null, "", "?" + p);
  }

  // ---------------------------------------------------------------- list ---
  async function loadEngineers() {
    const list = await api("/api/v1/engineers");
    $("engineer").innerHTML = list.map((e) => `<option value="${e.engineer_id}">${esc(e.name)}</option>`).join("");
    if (!list.some((e) => e.engineer_id === state.engineer)) state.engineer = list[0]?.engineer_id || "";
    $("engineer").value = state.engineer;
  }

  async function renderList() {
    const trips = await api(`/api/v1/trips?engineer_id=${encodeURIComponent(state.engineer)}`);
    $("no-trips").classList.toggle("hidden", trips.length > 0);
    $("trips").innerHTML = trips.map(({ trip: t, summary: s }) => `
      <button class="card trip-card" data-trip="${t.trip_id}">
        <div class="row-between"><span class="muted small">${t.trip_id}</span><span class="pill ${t.status}">${STATUS_TEXT[t.status]}</span></div>
        <h4>${esc(t.store_name)}</h4>
        <div class="muted">${esc(t.city)}, ${esc(t.country)} · ${esc(t.code)}</div>
        <div class="meta"><span>⏱ ${s.total_onsite_hours.toFixed(2)} h</span><span>🧾 ${s.expenses.receipt_count} · ${eur(s.expenses.total_eur)}</span>
        ${t.required_parts?.length ? `<span>📦 ${t.required_parts.map(esc).join(", ")}</span>` : ""}</div>
      </button>`).join("");
    $("trips").querySelectorAll("[data-trip]").forEach((b) => b.addEventListener("click", () => openTrip(b.dataset.trip)));
  }

  // ---------------------------------------------------------------- trip ---
  async function openTrip(id) {
    state.tripId = id;
    state.editing.clear();
    syncUrl();
    $("list-view").classList.add("hidden");
    $("trip-view").classList.remove("hidden");
    window.scrollTo(0, 0);
    await refreshTrip();
  }

  function closeTrip() {
    state.tripId = "";
    state.detail = null;
    syncUrl();
    $("trip-view").classList.add("hidden");
    $("list-view").classList.remove("hidden");
    renderList();
  }

  async function refreshTrip() {
    if (!state.tripId) return;
    try {
      state.detail = await api(`/api/v1/trips/${encodeURIComponent(state.tripId)}`);
    } catch (e) {
      toast(e.message);
      return closeTrip();
    }
    renderTrip();
    renderReminder();
  }

  function renderTrip() {
    const { trip: t, summary: s, expenses } = state.detail;
    $("t-id").textContent = t.trip_id;
    $("t-store").textContent = t.store_name;
    $("t-city").textContent = `${t.city}, ${t.country}`;
    $("t-status").className = "pill " + t.status;
    $("t-status").textContent = STATUS_TEXT[t.status];
    $("t-code").textContent = t.code;
    $("t-ticket").textContent = t.ticket_id;
    $("t-parts").innerHTML = (t.required_parts || []).map((p) => `<span>📦 ${esc(p)}</span>`).join("");

    // timesheet
    const locked = t.status === "SUBMITTED" || t.status === "APPROVED";
    const onSite = t.status === "ON_SITE";
    const punch = $("punch");
    punch.textContent = onSite ? "Leave site (check out)" : "Arrived on site (check in)";
    punch.classList.toggle("out", onSite);
    punch.disabled = locked;
    $("punches").innerHTML = t.punches.map((p) => {
      const d = p.distance_to_store_km;
      const geo = d == null ? `<span class="muted">no GPS</span>` : d > 1 ? `<span class="warn">${d.toFixed(1)} km from store</span>` : `<span class="muted">at store (${d.toFixed(2)} km)</span>`;
      return `<li><span>${p.action === "CHECK_IN" ? "▶ In" : "■ Out"} · ${dateTime(p.at)}</span>${geo}</li>`;
    }).join("");
    tickClock();

    // receipts
    $("exp-total").textContent = eur(s.expenses.total_eur);
    ["cam", "file", "samples-btn"].forEach((id) => ($(id).disabled = t.status === "APPROVED"));
    $("expenses").innerHTML = expenses.length ? expenses.map(renderExpense).join("") : `<p class="muted small">No receipts yet. Snap a photo of each receipt — the amounts are read automatically.</p>`;
    bindExpenseActions();

    // submit
    const submit = $("submit");
    submit.disabled = t.status !== "WORK_DONE";
    submit.textContent = t.status === "APPROVED" ? "Approved ✔" : t.status === "SUBMITTED" ? "Submitted — waiting for approval" : "Submit hours & receipts";
    $("submit-note").textContent = t.status === "WORK_DONE" ? `${s.total_onsite_hours.toFixed(2)} h on site · ${s.expenses.receipt_count} receipts · ${eur(s.expenses.total_eur)}`
      : t.status === "ON_SITE" || t.status === "ASSIGNED" ? "Check out before submitting." : "";
  }

  function renderExpense(e) {
    const r = e.final || {};
    const img = `<img class="thumb" src="/api/v1/expenses/${e.expense_id}/image" data-full="/api/v1/expenses/${e.expense_id}/image" alt="receipt">`;
    const pill = `<span class="pill ${e.status}">${e.status === "ANALYZING" ? '<span class="spinner"></span>' : ""}${STATUS_TEXT[e.status]}</span>`;
    if (e.status === "ANALYZING") {
      return `<div class="expense">${img}<div><div class="top"><span class="merchant muted">${esc(e.source_name || "Receipt")}</span>${pill}</div>
        <div class="sub">AI is reading the receipt…</div></div></div>`;
    }
    const editing = state.editing.has(e.expense_id) || e.status === "FAILED";
    const locked = state.detail.trip.status === "APPROVED";
    let body = `<div class="top"><span class="merchant">${esc(r.merchant_name || "—")}</span>${pill}</div>
      <div class="amount">${r.total_amount != null ? eur(r.total_amount) : "—"}</div>
      <div class="sub">${esc(r.expense_category || "")} · ${esc(r.transaction_date || "")}${r.transaction_time ? " " + esc(r.transaction_time) : ""}${r.vat_amount ? ` · VAT ${eur(r.vat_amount)}` : ""}</div>
      ${r.travel_segment ? `<div class="sub">✈ ${esc(r.travel_segment.from)} → ${esc(r.travel_segment.to)} ${esc((r.travel_segment.departure_time || "").slice(11))}–${esc((r.travel_segment.arrival_time || "").slice(11))}</div>` : ""}
      ${r.line_items_summary ? `<div class="sub">${esc(r.line_items_summary)}</div>` : ""}
      ${e.status === "NEEDS_REVIEW" ? `<div class="reason">⚠ ${esc(r.review_reason || "Low confidence")} (confidence ${(r.confidence_score ?? 0).toFixed(2)})</div>` : ""}
      ${e.error ? `<div class="err">${esc(e.error)}</div>` : ""}
      <div class="sub">${esc(e.model || "")}${e.latency_ms ? ` · ${(e.latency_ms / 1000).toFixed(1)} s` : ""}${e.edited_by_engineer ? " · corrected by you" : ""}</div>`;
    if (editing && !locked) {
      body += `<form class="edit" data-exp="${e.expense_id}">
        <label class="full">Merchant<input name="merchant_name" value="${esc(r.merchant_name)}"></label>
        <label>Total (EUR)<input name="total_amount" type="number" step="0.01" inputmode="decimal" value="${r.total_amount ?? ""}" required></label>
        <label>VAT (EUR)<input name="vat_amount" type="number" step="0.01" inputmode="decimal" value="${r.vat_amount ?? 0}"></label>
        <label>Date<input name="transaction_date" type="date" value="${esc(r.transaction_date)}" required></label>
        <label>Category<select name="expense_category">${CATS.map((c) => `<option ${c === r.expense_category ? "selected" : ""}>${c}</option>`).join("")}</select></label>
        <div class="actions full"><button class="btn primary" type="submit">Save &amp; confirm</button>
        ${e.status !== "FAILED" ? `<button class="btn ghost" type="button" data-cancel="${e.expense_id}">Cancel</button>` : `<button class="btn ghost" type="button" data-retry="${e.expense_id}">Retry AI</button>`}</div>
      </form>`;
    } else if (!locked && e.status !== "CONFIRMED") {
      body += `<div class="actions">
        ${e.status === "NEEDS_REVIEW" || e.status === "EXTRACTED" ? `<button class="btn primary" data-confirm="${e.expense_id}">Looks right</button>` : ""}
        <button class="btn ghost" data-edit="${e.expense_id}">Edit</button></div>`;
    } else if (!locked) {
      body += `<div class="actions"><button class="btn ghost" data-edit="${e.expense_id}">Edit</button></div>`;
    }
    return `<div class="expense">${img}<div>${body}</div></div>`;
  }

  function bindExpenseActions() {
    const root = $("expenses");
    root.querySelectorAll("img.thumb").forEach((im) => im.addEventListener("click", () => openLightbox(im.dataset.full)));
    root.querySelectorAll("[data-edit]").forEach((b) => b.addEventListener("click", () => { state.editing.add(b.dataset.edit); renderTrip(); }));
    root.querySelectorAll("[data-cancel]").forEach((b) => b.addEventListener("click", () => { state.editing.delete(b.dataset.cancel); renderTrip(); }));
    root.querySelectorAll("[data-retry]").forEach((b) => b.addEventListener("click", async () => {
      try { await post(`/api/v1/expenses/${b.dataset.retry}/retry`); } catch (e) { toast(e.message); }
      refreshTrip();
    }));
    root.querySelectorAll("[data-confirm]").forEach((b) => b.addEventListener("click", async () => {
      try { await post(`/api/v1/expenses/${b.dataset.confirm}/confirm`, { engineer_id: state.engineer }); toast("Receipt confirmed"); } catch (e) { toast(e.message); }
      refreshTrip();
    }));
    // once the engineer starts typing, stop the poll from re-rendering the form
    root.querySelectorAll("form.edit").forEach((f) => f.addEventListener("focusin", () => state.editing.add(f.dataset.exp)));
    root.querySelectorAll("form.edit").forEach((f) => f.addEventListener("submit", async (ev) => {
      ev.preventDefault();
      const id = f.dataset.exp;
      const prev = state.detail.expenses.find((x) => x.expense_id === id)?.final || {};
      const fd = new FormData(f);
      const receipt = { ...prev, currency: "EUR",
        merchant_name: fd.get("merchant_name"), total_amount: parseFloat(fd.get("total_amount")),
        vat_amount: parseFloat(fd.get("vat_amount") || "0"), transaction_date: fd.get("transaction_date"),
        expense_category: fd.get("expense_category"), flag_manual_review: false };
      try {
        await post(`/api/v1/expenses/${id}/confirm`, { engineer_id: state.engineer, receipt });
        state.editing.delete(id);
        toast("Receipt saved");
      } catch (e) { toast(e.message); }
      refreshTrip();
    }));
  }

  // ------------------------------------------------------------ actions ---
  // The browser's own timeout only starts after the permission prompt is
  // answered, so cap the whole wait: a punch must never hang on GPS.
  function getPosition() {
    return new Promise((resolve) => {
      if (!navigator.geolocation) return resolve(null);
      setTimeout(() => resolve(null), 10000);
      navigator.geolocation.getCurrentPosition(
        (p) => resolve(p.coords),
        () => resolve(null),
        { enableHighAccuracy: true, timeout: 8000, maximumAge: 30000 });
    });
  }

  async function doPunch() {
    const t = state.detail.trip;
    const action = t.status === "ON_SITE" ? "CHECK_OUT" : "CHECK_IN";
    $("punch").disabled = true;
    $("geo-note").textContent = "Getting location…";
    const c = await getPosition();
    $("geo-note").textContent = c ? `Location ±${Math.round(c.accuracy)} m` : "Location unavailable — recorded without GPS (needs HTTPS on a phone).";
    try {
      await post("/api/v1/engineers/timesheets/punch", {
        engineer_id: state.engineer, project_trip_id: t.trip_id, store_id: t.store_id, action,
        timestamp: Math.floor(Date.now() / 1000), geo_lat: c?.latitude, geo_lng: c?.longitude, geo_accuracy_m: c?.accuracy || 0,
      });
      toast(action === "CHECK_IN" ? "Checked in — timer started" : "Checked out — hours saved");
    } catch (e) { toast(e.message); }
    refreshTrip();
  }

  // Downscale on the device: smaller upload, fewer LLM tokens, and converts
  // HEIC photos (iPhone) to JPEG. Falls back to the original file.
  async function prepareImage(file) {
    try {
      const bmp = await createImageBitmap(file);
      const scale = Math.min(1, 1800 / Math.max(bmp.width, bmp.height));
      const cv = document.createElement("canvas");
      cv.width = Math.round(bmp.width * scale);
      cv.height = Math.round(bmp.height * scale);
      cv.getContext("2d").drawImage(bmp, 0, 0, cv.width, cv.height);
      const blob = await new Promise((res) => cv.toBlob(res, "image/jpeg", 0.88));
      return blob ? new File([blob], (file.name || "receipt").replace(/\.\w+$/, "") + ".jpg", { type: "image/jpeg" }) : file;
    } catch {
      return file;
    }
  }

  async function uploadFile(file) {
    if (!file) return;
    toast("Uploading…");
    const fd = new FormData();
    fd.append("engineer_id", state.engineer);
    fd.append("file", await prepareImage(file));
    try {
      await api(`/api/v1/trips/${encodeURIComponent(state.tripId)}/expenses`, { method: "POST", body: fd });
      toast("Receipt uploaded — AI is reading it");
    } catch (e) { toast(e.message); }
    refreshTrip();
  }

  async function toggleSamples() {
    const box = $("samples");
    if (!state.samplesLoaded) {
      const names = await api("/api/v1/erp/sample-receipts");
      box.innerHTML = names.map((n) => `<button title="${esc(n)}" data-sample="${esc(n)}"><img src="/api/v1/erp/sample-receipts/${encodeURIComponent(n)}" alt="${esc(n)}" loading="lazy"></button>`).join("")
        || `<p class="muted small">No sample receipts found.</p>`;
      box.querySelectorAll("[data-sample]").forEach((b) => b.addEventListener("click", async () => {
        try {
          await post(`/api/v1/trips/${encodeURIComponent(state.tripId)}/expenses/sample`, { name: b.dataset.sample, engineer_id: state.engineer });
          toast("Sample uploaded — AI is reading it");
        } catch (e) { toast(e.message); }
        refreshTrip();
      }));
      state.samplesLoaded = true;
    }
    box.classList.toggle("hidden");
  }

  async function doSubmit() {
    try {
      await post(`/api/v1/trips/${encodeURIComponent(state.tripId)}/submit`, { engineer_id: state.engineer });
      toast("Submitted for approval");
    } catch (e) { toast(e.message); }
    refreshTrip();
  }

  async function renderReminder() {
    const mails = await api("/api/v1/erp/outbox").catch(() => []);
    const m = mails.find((x) => x.trip_id === state.tripId);
    const el = $("reminder");
    el.classList.toggle("hidden", !m);
    if (m) el.textContent = `📧 ${m.subject}\n${time(m.sent_at)} · sent to ${m.to}`;
  }

  function openLightbox(src) {
    const lb = $("lightbox");
    lb.querySelector("img").src = src;
    lb.classList.remove("hidden");
  }

  // ------------------------------------------------------------ clock ---
  function tickClock() {
    const d = state.detail;
    if (!d) return;
    let ms = 0, inAt = null;
    for (const p of d.trip.punches) {
      if (p.action === "CHECK_IN") inAt = new Date(p.at);
      else if (inAt) { ms += new Date(p.at) - inAt; inAt = null; }
    }
    if (inAt) ms += Date.now() - inAt;
    const s = Math.max(0, Math.floor(ms / 1000));
    $("clock").textContent = `${Math.floor(s / 3600)}:${String(Math.floor(s / 60) % 60).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
  }

  // ------------------------------------------------------------- wiring ---
  $("engineer").addEventListener("change", (e) => { state.engineer = e.target.value; closeTrip(); });
  $("back").addEventListener("click", closeTrip);
  $("punch").addEventListener("click", doPunch);
  $("submit").addEventListener("click", doSubmit);
  $("samples-btn").addEventListener("click", toggleSamples);
  $("cam").addEventListener("change", (e) => { uploadFile(e.target.files[0]); e.target.value = ""; });
  $("file").addEventListener("change", (e) => { uploadFile(e.target.files[0]); e.target.value = ""; });
  $("lightbox").addEventListener("click", () => $("lightbox").classList.add("hidden"));

  setInterval(tickClock, 1000);
  setInterval(() => {
    if (document.hidden) return;
    if (state.tripId) {
      // keep polling while the AI is reading or a reminder may arrive; never clobber an open edit form
      if (state.editing.size === 0) refreshTrip();
    } else {
      renderList();
    }
  }, 3000);

  loadEngineers().then(() => (state.tripId ? openTrip(state.tripId) : (syncUrl(), renderList())));
})();
