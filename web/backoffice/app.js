// Rocateq back office: dispatch, trip review, approval, outbox.
(() => {
  const $ = (id) => document.getElementById(id);
  const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  const eur = (n) => "€" + Number(n || 0).toFixed(2);
  const dt = (iso) => (iso ? new Date(iso).toLocaleString([], { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" }) : "");
  const state = { selected: new URLSearchParams(location.search).get("trip_id") || "", engineers: [], picks: {} };

  async function api(path, opts) {
    const r = await fetch(path, opts);
    const body = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(body.error || r.statusText);
    return body;
  }
  const post = (path, data) => api(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(data || {}) });

  async function refresh() {
    const [queue, trips, outbox] = await Promise.all([api("/api/v1/dispatch/queue"), api("/api/v1/trips"), api("/api/v1/erp/outbox")]);
    renderQueue(queue);
    renderTrips(trips);
    renderOutbox(outbox);
    const sel = trips.find((d) => d.trip.trip_id === state.selected);
    sel ? renderDetail(sel) : $("detail").classList.add("hidden");
  }

  function renderQueue(queue) {
    $("q-count").textContent = queue.length || "";
    if (!queue.length) {
      $("queue").innerHTML = `<tr class="empty"><td colspan="9">No tickets waiting for dispatch.</td></tr>`;
      return;
    }
    const opts = state.engineers.map((e) => `<option value="${e.engineer_id}">${esc(e.name)}</option>`).join("");
    $("queue").innerHTML = queue.map((t) => `<tr>
      <td><strong>${t.ticket_id}</strong></td><td>${esc(t.store_id)}<br><span class="muted small">${esc(t.controller_id)}</span></td>
      <td>${esc(t.code)}</td><td>${(t.required_parts || []).map((p) => `<span class="part">${esc(p)}</span>`).join("") || "—"}</td>
      <td><span class="pill ${t.source}">${t.source === "AUTO" ? "Rule engine" : "Store manager"}</span></td>
      <td class="num">${t.wizard_attempts}</td><td>${dt(t.created_at)}</td>
      <td><select data-pick="${t.ticket_id}">${opts}</select></td>
      <td><button class="btn primary" data-dispatch="${t.ticket_id}">Dispatch</button></td></tr>`).join("");
    $("queue").querySelectorAll("select[data-pick]").forEach((s) => {
      if (state.picks[s.dataset.pick]) s.value = state.picks[s.dataset.pick];
      s.addEventListener("change", () => (state.picks[s.dataset.pick] = s.value));
    });
    $("queue").querySelectorAll("[data-dispatch]").forEach((b) => b.addEventListener("click", async () => {
      const id = b.dataset.dispatch;
      const eng = $("queue").querySelector(`select[data-pick="${id}"]`).value;
      b.disabled = true;
      try {
        const trip = await post("/api/v1/trips", { ticket_id: id, engineer_id: eng });
        state.selected = trip.trip_id;
      } catch (e) { alertInline(e.message); }
      refresh();
    }));
  }

  function renderTrips(trips) {
    if (!trips.length) {
      $("trips").innerHTML = `<tr class="empty"><td colspan="9">No trips yet — dispatch a ticket above.</td></tr>`;
      return;
    }
    $("trips").innerHTML = trips.map(({ trip: t, summary: s, engineer: e }) => `
      <tr class="clickable ${t.trip_id === state.selected ? "selected" : ""}" data-trip="${t.trip_id}">
        <td><strong>${t.trip_id}</strong></td><td><span class="pill ${s.status}">${s.status.replaceAll("_", " ")}</span></td>
        <td>${esc(e.name)}</td><td>${esc(t.store_name)}<br><span class="muted small">${esc(t.code)}</span></td>
        <td class="num">${s.total_onsite_hours.toFixed(2)}${s.on_site_now ? " ●" : ""}</td><td class="num">${eur(s.expenses.total_eur)}</td>
        <td class="num">${s.expenses.receipt_count}</td><td class="num ${s.expenses.receipts_pending_review ? "warn" : ""}">${s.expenses.receipts_pending_review}</td>
        <td class="num ${s.geo_warnings ? "warn" : ""}">${s.geo_warnings}</td></tr>`).join("");
    $("trips").querySelectorAll("[data-trip]").forEach((r) => r.addEventListener("click", () => {
      state.selected = r.dataset.trip;
      history.replaceState(null, "", "?trip_id=" + state.selected);
      refresh();
    }));
  }

  function renderDetail({ trip: t, summary: s, engineer: e, expenses }) {
    $("detail").classList.remove("hidden");
    $("d-id").textContent = `${t.trip_id} · ticket ${t.ticket_id}`;
    $("d-title").textContent = `${t.store_name} — ${e.name}`;
    $("d-sub").textContent = `${t.city}, ${t.country} · ${t.code} · parts: ${(t.required_parts || []).join(", ") || "—"}`;
    $("d-status").className = "pill " + s.status;
    $("d-status").textContent = s.status.replaceAll("_", " ");
    const approve = $("approve");
    approve.disabled = s.status !== "READY_FOR_APPROVAL";
    approve.textContent = s.status === "APPROVED" ? `Approved by ${t.approved_by}` : "Approve";
    approve.title = s.status === "AWAITING_RECEIPT_REVIEW" ? "Some receipts still need review" : s.status === "IN_PROGRESS" ? "Engineer has not submitted yet" : "";

    $("k-hours").textContent = s.total_onsite_hours.toFixed(2) + " h" + (s.on_site_now ? " ●" : "");
    $("k-eur").textContent = eur(s.expenses.total_eur);
    $("k-vat").textContent = `incl. VAT ${eur(s.expenses.vat_eur)} · all amounts treated as EUR`;
    $("k-receipts").textContent = s.expenses.receipt_count;
    $("k-pending").textContent = s.expenses.receipts_pending_review ? `${s.expenses.receipts_pending_review} need review` : "all checked";
    const env = s.receipt_envelope;
    $("k-env").textContent = env ? env.span_hours.toFixed(1) + " h" : "—";
    $("k-env-range").textContent = env ? `${env.first} → ${env.last}` : "needs ≥ 2 timed receipts";
    $("calib-note").textContent = env
      ? `Discussion item (hours calibration): receipts span ${env.span_hours.toFixed(1)} h door-to-door vs. ${s.total_onsite_hours.toFixed(2)} h punched on site. v1 pays on-site punches only; travel hours from receipts are shown for comparison, not used.`
      : "";

    $("d-punches").innerHTML = t.punches.length ? t.punches.map((p) => {
      const d = p.distance_to_store_km;
      return `<tr><td>${p.action === "CHECK_IN" ? "Check-in" : "Check-out"}</td><td>${dt(p.at)}</td>
        <td>${d == null ? '<span class="muted">no GPS</span>' : d > 1 ? `<span class="warn">${d.toFixed(1)} km from store</span>` : `${d.toFixed(2)} km`}</td></tr>`;
    }).join("") : `<tr class="empty"><td colspan="3">No punches yet.</td></tr>`;

    const cats = Object.entries(s.expenses.by_category || {}).sort((a, b) => b[1] - a[1]);
    $("d-cats").innerHTML = cats.length ? cats.map(([c, v]) => `<tr><td>${c}</td><td class="num">${eur(v)}</td></tr>`).join("") : `<tr class="empty"><td>—</td></tr>`;

    $("d-expenses").innerHTML = expenses.length ? expenses.map((x) => {
      const r = x.final || {};
      const raw = x.extracted;
      const changed = x.edited_by_engineer && raw ? `<div class="raw">AI read: ${eur(raw.total_amount)} · ${esc(raw.expense_category)}</div>` : "";
      return `<tr>
        <td><img class="thumb" src="/api/v1/expenses/${x.expense_id}/image" alt=""></td>
        <td><span class="pill ${x.status}">${x.status.replaceAll("_", " ")}</span></td>
        <td>${esc(r.merchant_name || x.source_name || "—")}${r.line_items_summary ? `<div class="raw">${esc(r.line_items_summary)}</div>` : ""}</td>
        <td>${esc(r.transaction_date || "")} ${esc(r.transaction_time || "")}${r.travel_segment ? `<div class="raw">${esc(r.travel_segment.from)} → ${esc(r.travel_segment.to)}</div>` : ""}</td>
        <td>${esc(r.expense_category || "")}</td>
        <td class="num">${r.total_amount != null ? eur(r.total_amount) : "—"}${changed}</td>
        <td class="num">${r.vat_amount != null ? eur(r.vat_amount) : ""}</td>
        <td class="num">${raw ? raw.confidence_score.toFixed(2) : "—"}</td>
        <td class="small">${x.edited_by_engineer ? "✎ corrected by engineer" : ""}${raw?.review_reason ? `<div class="warn">${esc(raw.review_reason)}</div>` : ""}${x.error ? `<div class="warn">${esc(x.error)}</div>` : ""}
          <div class="raw">${esc(x.model || "")}${x.latency_ms ? ` · ${(x.latency_ms / 1000).toFixed(1)} s` : ""}</div></td></tr>`;
    }).join("") : `<tr class="empty"><td colspan="9">No receipts uploaded yet.</td></tr>`;
    $("d-expenses").querySelectorAll("img.thumb").forEach((im) => im.addEventListener("click", () => {
      $("lightbox").querySelector("img").src = im.src;
      $("lightbox").classList.remove("hidden");
    }));
  }

  function renderOutbox(mails) {
    $("outbox").innerHTML = mails.length ? mails.slice(0, 20).map((m) => `<div class="mail">
      <div class="hdr"><span>To: ${esc(m.to)} · ${esc(m.kind)}</span><span>${dt(m.sent_at)}</span></div>
      <div class="subj">${esc(m.subject)}</div><pre>${linkify(esc(m.body))}</pre></div>`).join("") : `<p class="muted">No e-mails sent yet.</p>`;
  }

  const linkify = (s) => s.replace(/(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank">$1</a>');

  function alertInline(msg) {
    const n = $("calib-note");
    console.warn(msg);
    n.textContent = msg;
  }

  $("approve").addEventListener("click", async () => {
    try { await post(`/api/v1/trips/${encodeURIComponent(state.selected)}/approve`, { approver: "Derrick van Welie" }); } catch (e) { alertInline(e.message); }
    refresh();
  });
  $("lightbox").addEventListener("click", () => $("lightbox").classList.add("hidden"));

  api("/api/v1/engineers").then((list) => { state.engineers = list; refresh(); setInterval(() => !document.hidden && refresh(), 3000); });
})();
