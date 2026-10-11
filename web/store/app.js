// Rocateq store-manager UI. No build step: plain JS polling the Go API.
(() => {
  const POLL_MS = 3000;
  const ICONS = {
    cabinet_open: "🗄️", cable_plug: "🔌", power_plug: "⚡", network_cable: "🌐",
    refresh_check: "🔄", technician: "🧑‍🔧", info: "ℹ️",
  };

  // UI chrome strings. Guide content itself comes from the LLM module.
  const I18N = {
    en: {
      assistant: "Store Assistant", devices: "Your Rocateq devices", ticketTitle: "Rocateq support is on it",
      HEALTHY: "All systems working", ACTION_REQUIRED: "Action needed — you can fix this",
      ESCALATED: "Technician notified", SERVICE_REQUIRED: "Technician needed", OFFLINE: "Box not reachable", UNKNOWN: "Connecting…",
      healthySub: "Cart protection is active. Nothing to do.", since: "since",
      verify: "Verify solution", verifying: "Testing the system…", escalate: "Still not working? Request support",
      escalated: "Support requested", ok: "✅ Fixed! The system is working again.", still: "Still a problem — check the steps again.",
      noResp: "The box did not answer. Check its power light and network cable.",
      ticketText: (id, parts) => `Ticket ${id} has been created${parts ? ` — the technician will bring: ${parts}` : ""}. No need to call us.`,
      lastSeen: "last report", never: "no report yet", issue: "Issue", attempts: "verify attempts", guideBy: "guide by",
    },
    nl: {
      assistant: "Winkelassistent", devices: "Uw Rocateq-apparaten", ticketTitle: "Rocateq-support is ermee bezig",
      HEALTHY: "Alles werkt", ACTION_REQUIRED: "Actie nodig — u kunt dit zelf oplossen",
      ESCALATED: "Monteur ingelicht", SERVICE_REQUIRED: "Monteur nodig", OFFLINE: "Kast niet bereikbaar", UNKNOWN: "Verbinden…",
      healthySub: "Winkelwagenbeveiliging is actief. U hoeft niets te doen.", since: "sinds",
      verify: "Opnieuw testen", verifying: "Systeem wordt getest…", escalate: "Werkt het nog niet? Vraag support",
      escalated: "Support aangevraagd", ok: "✅ Opgelost! Het systeem werkt weer.", still: "Nog steeds een probleem — loop de stappen nog eens na.",
      noResp: "De kast antwoordt niet. Controleer het stroomlampje en de netwerkkabel.",
      ticketText: (id, parts) => `Ticket ${id} is aangemaakt${parts ? ` — de monteur neemt mee: ${parts}` : ""}. U hoeft ons niet te bellen.`,
      lastSeen: "laatste melding", never: "nog geen melding", issue: "Melding", attempts: "testpogingen", guideBy: "gids door",
    },
    fr: {
      assistant: "Assistant magasin", devices: "Vos appareils Rocateq", ticketTitle: "Le support Rocateq s'en occupe",
      HEALTHY: "Tout fonctionne", ACTION_REQUIRED: "Action requise — vous pouvez le réparer",
      ESCALATED: "Technicien prévenu", SERVICE_REQUIRED: "Technicien nécessaire", OFFLINE: "Boîtier injoignable", UNKNOWN: "Connexion…",
      healthySub: "La protection des chariots est active. Rien à faire.", since: "depuis",
      verify: "Vérifier", verifying: "Test du système…", escalate: "Toujours en panne ? Demander de l'aide",
      escalated: "Aide demandée", ok: "✅ Réparé ! Le système fonctionne de nouveau.", still: "Toujours un problème — revérifiez les étapes.",
      noResp: "Le boîtier ne répond pas. Vérifiez son voyant et le câble réseau.",
      ticketText: (id, parts) => `Le ticket ${id} a été créé${parts ? ` — le technicien apportera : ${parts}` : ""}. Inutile de nous appeler.`,
      lastSeen: "dernier rapport", never: "aucun rapport", issue: "Incident", attempts: "tentatives", guideBy: "guide par",
    },
    de: {
      assistant: "Markt-Assistent", devices: "Ihre Rocateq-Geräte", ticketTitle: "Der Rocateq-Support kümmert sich",
      HEALTHY: "Alles in Ordnung", ACTION_REQUIRED: "Handlung nötig — Sie können das selbst beheben",
      ESCALATED: "Techniker informiert", SERVICE_REQUIRED: "Techniker nötig", OFFLINE: "Box nicht erreichbar", UNKNOWN: "Verbinde…",
      healthySub: "Die Einkaufswagensicherung ist aktiv. Nichts zu tun.", since: "seit",
      verify: "Lösung prüfen", verifying: "System wird getestet…", escalate: "Klappt nicht? Support anfordern",
      escalated: "Support angefordert", ok: "✅ Behoben! Das System läuft wieder.", still: "Problem besteht weiter — bitte Schritte erneut prüfen.",
      noResp: "Die Box antwortet nicht. Prüfen Sie Betriebsanzeige und Netzwerkkabel.",
      ticketText: (id, parts) => `Ticket ${id} wurde erstellt${parts ? ` — der Techniker bringt mit: ${parts}` : ""}. Kein Anruf nötig.`,
      lastSeen: "letzte Meldung", never: "noch keine Meldung", issue: "Vorfall", attempts: "Prüfversuche", guideBy: "Anleitung von",
    },
  };

  const COLOR = {
    HEALTHY: "green", ACTION_REQUIRED: "red", OFFLINE: "red",
    ESCALATED: "orange", SERVICE_REQUIRED: "orange", UNKNOWN: "",
  };

  const $ = (id) => document.getElementById(id);
  const params = new URLSearchParams(location.search);
  const state = { store: params.get("store_id") || "", lang: params.get("lang") || "", data: null, busy: false, done: {}, result: null };

  const t = (k) => (I18N[state.data?.language] || I18N.en)[k] ?? I18N.en[k];
  const fmtTime = (iso) => iso ? new Date(iso).toLocaleTimeString() : "";

  async function api(path, opts) {
    const r = await fetch(path, opts);
    if (!r.ok) throw new Error(`${r.status} ${await r.text()}`);
    return r.json();
  }

  async function loadStores() {
    const stores = await api("/api/v1/stores");
    $("store").innerHTML = stores.map((s) => `<option value="${s.store_id}">${s.name} — ${s.city}</option>`).join("");
    if (!state.store || !stores.some((s) => s.store_id === state.store)) state.store = stores[0]?.store_id;
    $("store").value = state.store;
    $("lang").value = state.lang;
  }

  async function refresh() {
    if (!state.store || state.busy) return;
    const q = state.lang ? `?lang=${state.lang}` : "";
    try {
      state.data = await api(`/api/v1/stores/${encodeURIComponent(state.store)}/diagnostics/wizard${q}`);
      render();
    } catch (e) {
      $("status-title").textContent = "Connection error";
      $("status-sub").textContent = e.message;
    }
  }

  function render() {
    const d = state.data;
    const issue = d.active_issue;
    document.documentElement.lang = d.language;
    document.querySelectorAll("[data-i18n]").forEach((el) => (el.textContent = t(el.dataset.i18n)));

    // Traffic light
    $("light").className = "light " + (COLOR[d.system_status] || "");
    $("store-name").textContent = d.store_name;
    $("status-title").textContent = t(d.system_status);
    $("status-sub").textContent = issue
      ? `${t("since")} ${fmtTime(issue.detected_at)}`
      : d.system_status === "HEALTHY" ? t("healthySub") : "";

    // Wizard
    const g = issue?.wizard_data;
    $("wizard").classList.toggle("hidden", !g);
    if (g) {
      const doneSet = state.done[issue.issue_id] || (state.done[issue.issue_id] = new Set());
      $("guide-title").textContent = g.title;
      $("fix-time").textContent = "⏱ " + g.estimated_fix_time;
      $("steps").innerHTML = g.steps.map((s) => `
        <li class="step ${doneSet.has(s.step_number) ? "done" : ""}" data-step="${s.step_number}">
          <div class="icon">${doneSet.has(s.step_number) ? "✔️" : ICONS[s.visual_hint_icon] || "•"}</div>
          <div><h4><span class="num">${s.step_number}.</span>${escapeHtml(s.title)}</h4><p>${escapeHtml(s.instruction)}</p></div>
        </li>`).join("");
      $("steps").querySelectorAll(".step").forEach((el) => el.addEventListener("click", () => {
        const n = Number(el.dataset.step);
        doneSet.has(n) ? doneSet.delete(n) : doneSet.add(n);
        render();
      }));
      $("verify").classList.toggle("hidden", !issue.can_self_resolve);
      $("verify").textContent = t("verify");
      const escalated = !!issue.ticket_id;
      $("escalate").textContent = escalated ? t("escalated") : t("escalate");
      $("escalate").disabled = escalated;
      $("escalate").classList.toggle("hidden", !issue.can_self_resolve && escalated);
      $("meta").textContent = `${t("issue")} ${issue.issue_id} · ${issue.code} · ${issue.verify_attempts} ${t("attempts")} · ${t("guideBy")} ${g.source}`;
    }

    // Result banner survives one refresh after a verify
    const res = $("result");
    if (state.result) {
      res.className = "result " + state.result.cls;
      res.textContent = state.result.text;
    } else {
      res.className = "result hidden";
    }

    // Ticket
    const ticketId = issue?.ticket_id;
    $("ticket-card").classList.toggle("hidden", !ticketId);
    if (ticketId) {
      const parts = d.controllers.flatMap((c) => c.findings).flatMap((f) => f.required_parts || []);
      $("ticket-text").textContent = t("ticketText")(ticketId, [...new Set(parts)].join(", "));
    }

    // Devices
    $("devices").innerHTML = d.controllers.map((c) => `
      <li>
        <span class="dot ${COLOR[c.status] || ""}"></span>
        <div class="grow"><strong>${c.controller_id}</strong> <small>${escapeHtml(c.model)}</small><br>
          <small>${escapeHtml(c.location || "")}</small></div>
        <small>${c.last_seen ? `${t("lastSeen")} ${fmtTime(c.last_seen)}` : t("never")}</small>
      </li>`).join("");

    $("foot").textContent = `Rocateq remote diagnostics demo · ${d.store_id} · LLM: ${d.llm_provider}`;
  }

  async function verify() {
    const issue = state.data?.active_issue;
    if (!issue) return;
    state.busy = true;
    $("verify").disabled = true;
    state.result = { cls: "wait", text: t("verifying") };
    render();
    try {
      const r = await api(`/api/v1/stores/${encodeURIComponent(state.store)}/diagnostics/wizard/verify`, {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ issue_id: issue.issue_id, controller_id: issue.controller_id }),
      });
      state.result = r.result === "RESOLVED" ? { cls: "ok", text: t("ok") }
        : r.result === "NO_RESPONSE" ? { cls: "bad", text: t("noResp") }
        : { cls: "bad", text: t("still") };
    } catch (e) {
      state.result = { cls: "bad", text: e.message };
    } finally {
      state.busy = false;
      $("verify").disabled = false;
      await refresh();
      if (state.result?.cls === "ok") showHealthyResult();
      setTimeout(() => { state.result = null; }, 8000);
    }
  }

  // After a successful verify the wizard disappears; show the success on the status card.
  function showHealthyResult() {
    if (state.data && !state.data.active_issue) $("status-sub").textContent = t("ok");
  }

  async function escalate() {
    const issue = state.data?.active_issue;
    $("escalate").disabled = true;
    try {
      await api(`/api/v1/stores/${encodeURIComponent(state.store)}/tickets/escalate`, {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ issue_id: issue?.issue_id || "" }),
      });
    } finally {
      await refresh();
    }
  }

  function escapeHtml(s) {
    return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  }

  function syncUrl() {
    const p = new URLSearchParams({ store_id: state.store });
    if (state.lang) p.set("lang", state.lang);
    history.replaceState(null, "", "?" + p);
  }

  $("store").addEventListener("change", (e) => { state.store = e.target.value; state.result = null; syncUrl(); refresh(); });
  $("lang").addEventListener("change", (e) => { state.lang = e.target.value; syncUrl(); refresh(); });
  $("verify").addEventListener("click", verify);
  $("escalate").addEventListener("click", escalate);

  loadStores().then(() => { syncUrl(); refresh(); setInterval(refresh, POLL_MS); });
})();
