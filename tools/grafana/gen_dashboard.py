"""Generate deploy/grafana/dashboards/rocateq-ops.json.

Usage: python tools/grafana/gen_dashboard.py deploy/grafana/dashboards/rocateq-ops.json
Grafana re-reads provisioned dashboards every ~10 s, no restart needed.
"""
import json, sys
DS = {"type": "yesoreyeram-infinity-datasource", "uid": "rocateq-api"}

def q(path, cols, ref="A"):
    return {"refId": ref, "datasource": DS, "type": "json", "source": "url", "format": "table",
            "parser": "backend", "url": "${api}" + path, "url_options": {"method": "GET", "data": ""},
            "root_selector": "", "columns": [{"selector": s, "text": t, "type": ty} for s, t, ty in cols], "filters": []}

pid = [0]
def panel(type_, title, grid, targets, **kw):
    pid[0] += 1
    p = {"id": pid[0], "type": type_, "title": title, "gridPos": dict(zip("xywh", grid)),
         "datasource": DS, "targets": targets}
    p.update(kw)
    return p

def thresholds(*steps):
    return {"mode": "absolute", "steps": [{"color": c, "value": v} for v, c in steps]}

def stat(title, field, x, color, unit="none", desc=""):
    return panel("stat", title, (x, 0, 4, 4), [q("/api/v1/ops/summary", [(field, title, "number")])],
        description=desc,
        fieldConfig={"defaults": {"unit": unit, "color": {"mode": "fixed", "fixedColor": color},
                                  "thresholds": thresholds((None, color))}, "overrides": []},
        options={"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
                 "colorMode": "background", "graphMode": "none", "textMode": "value", "justifyMode": "center"})

STATUS_MAP = [{"type": "value", "options": {
    "HEALTHY": {"color": "green", "index": 0, "text": "Healthy"},
    "UNKNOWN": {"color": "#8e8e8e", "index": 1, "text": "Unknown"},
    "ACTION_REQUIRED": {"color": "red", "index": 2, "text": "Store can self-fix"},
    "ESCALATED": {"color": "orange", "index": 3, "text": "Technician dispatched"},
    "SERVICE_REQUIRED": {"color": "orange", "index": 4, "text": "Technician needed"},
    "OFFLINE": {"color": "dark-red", "index": 5, "text": "Offline"},
    "OPEN": {"color": "red", "index": 6}, "RESOLVED": {"color": "green", "index": 7},
    "CLOSED": {"color": "green", "index": 8},
    "SELF_RESOLVED": {"color": "green", "index": 9, "text": "Self-resolved (trip avoided)"},
    "RESOLVED_BY_SERVICE": {"color": "blue", "index": 10, "text": "Resolved by service"},
    "SUPERSEDED": {"color": "#8e8e8e", "index": 11},
    "AUTO": {"color": "purple", "index": 12, "text": "Auto (rule engine)"},
    "STORE_MANAGER": {"color": "blue", "index": 13, "text": "Store manager"}}}]

def colored(*names):
    return [{"matcher": {"id": "byName", "options": n}, "properties": [
        {"id": "custom.cellOptions", "value": {"type": "color-background", "mode": "basic"}},
        {"id": "mappings", "value": STATUS_MAP}]} for n in names]

def table(title, grid, path, cols, colored_cols, **kw):
    return panel("table", title, grid, [q(path, cols)],
        fieldConfig={"defaults": {"custom": {"align": "auto", "filterable": True}}, "overrides": colored(*colored_cols)},
        transformations=[{"id": "organize", "options": {"indexByName": {c[1]: i for i, c in enumerate(cols)}}}],
        options={"showHeader": True, "cellHeight": "sm", "footer": {"show": False}}, **kw)

def ts(title, grid, metric, unit, extra=None, desc=""):
    custom = {"drawStyle": "line", "lineWidth": 2, "spanNulls": True, "showPoints": "never", "fillOpacity": 0}
    custom.update(extra or {})
    return panel("timeseries", title, grid,
        [q("/api/v1/ops/telemetry?metric=%s&from=${__from}&to=${__to}&step_ms=${__interval_ms}" % metric,
           [("time", "time", "timestamp_epoch"), ("series", "series", "string"), ("value", "value", "number")])],
        description=desc,
        transformations=[{"id": "partitionByValues", "options": {"fields": ["series"], "keepFields": False,
                                                                 "naming": {"asLabels": True}}}],
        fieldConfig={"defaults": {"unit": unit, "displayName": "${__field.labels.series}", "custom": custom,
                                  "color": {"mode": "palette-classic"}}, "overrides": []},
        options={"legend": {"displayMode": "table", "placement": "right", "calcs": ["lastNotNull"]},
                 "tooltip": {"mode": "multi", "sort": "desc"}})

panels = [
    stat("Healthy stores", "healthy", 0, "green"),
    stat("Store can self-fix", "action_required", 4, "red", desc="Open self-fixable issue; the store manager is being guided by the wizard."),
    stat("Technician dispatched", "escalated", 8, "orange", desc="Qualified ticket open (hardware fault or manager escalation)."),
    stat("Offline stores", "offline", 12, "dark-red"),
    stat("Site trips avoided", "trips_avoided", 16, "blue", desc="Issues closed by the store itself (SELF_RESOLVED)."),
    stat("Est. travel cost avoided", "est_savings_eur", 20, "purple", unit="currencyEUR", desc="trips avoided x AVOIDED_TRIP_COST_EUR"),
]

geomap = panel("geomap", "Store fleet map", (0, 4, 10, 12),
    [q("/api/v1/ops/stores", [("name", "Store", "string"), ("city", "City", "string"), ("status", "Status", "string"),
                              ("status_rank", "status_rank", "number"), ("latitude", "latitude", "number"),
                              ("longitude", "longitude", "number"), ("issue_code", "Issue", "string")])],
    fieldConfig={"defaults": {"color": {"mode": "thresholds"},
                              "thresholds": thresholds((None, "green"), (1, "#8e8e8e"), (2, "red"), (3, "orange"), (5, "dark-red"))},
                 "overrides": []},
    options={"view": {"id": "coords", "lat": 51.2, "lon": 4.8, "zoom": 4.8}, "controls": {"showZoom": True, "mouseWheelZoom": True},
             "basemap": {"type": "xyz", "name": "Esri Dark Gray", "config": {
                 "url": "https://services.arcgisonline.com/ArcGIS/rest/services/Canvas/World_Dark_Gray_Base/MapServer/tile/{z}/{y}/{x}",
                 "attribution": "Tiles &copy; Esri &mdash; Esri, DeLorme, NAVTEQ"}},
             "layers": [{"type": "markers", "name": "Stores", "tooltip": True,
                         "location": {"mode": "coords", "latitude": "latitude", "longitude": "longitude"},
                         "config": {"showLegend": False,
                                    "style": {"size": {"fixed": 12}, "opacity": 0.9, "symbol": {"mode": "fixed", "fixed": "img/icons/marker/circle.svg"},
                                              "color": {"field": "status_rank", "fixed": "green"},
                                              "text": {"mode": "field", "field": "Store", "fixed": ""},
                                              "textConfig": {"fontSize": 11, "offsetY": -18, "textAlign": "center", "textBaseline": "middle"}}}}],
             "tooltip": {"mode": "details"}})

fleet = table("Store fleet status", (10, 4, 14, 12), "/api/v1/ops/stores",
    [("status", "Status", "string"), ("name", "Store", "string"), ("store_id", "Store ID", "string"), ("city", "City", "string"),
     ("country", "Country", "string"), ("controllers", "Controllers", "number"), ("issue_code", "Active issue", "string"),
     ("issue_since", "Since", "timestamp"), ("last_seen", "Last report", "timestamp")], ["Status"])

volt = ts("Controller supply voltage", (0, 16, 12, 9), "voltage_mv", "mvolt",
          {"thresholdsStyle": {"mode": "line"}},
          desc="Below 10 V (red line) the engine raises POWER_SUPPLY_ABNORMAL.")
volt["fieldConfig"]["defaults"]["thresholds"] = thresholds((None, "transparent"), (10000, "red"))
volt["fieldConfig"]["defaults"]["min"] = 8000

loop = ts("Perimeter loop impedance", (12, 16, 12, 9), "loop_impedance", "ohm",
          {"scaleDistribution": {"type": "log", "log": 10}, "thresholdsStyle": {"mode": "line"}},
          desc="Above 10 kOhm (red line) a loop is treated as open circuit -> LOOP_CABLE_DISCONNECTED.")
loop["fieldConfig"]["defaults"]["thresholds"] = thresholds((None, "transparent"), (10000, "red"))

issues = table("Open issues", (0, 25, 16, 8), "/api/v1/ops/issues?state=open",
    [("state", "State", "string"), ("store_id", "Store", "string"), ("controller_id", "Controller", "string"),
     ("code", "Root cause", "string"), ("severity", "Severity", "string"), ("self_fixable", "Self-fixable", "string"),
     ("target", "Target", "string"), ("opened_at", "Opened", "timestamp"), ("duration_min", "Open (min)", "number"),
     ("verify_attempts", "Verify attempts", "number"), ("ticket_id", "Ticket", "string")], ["State"])

pie = panel("piechart", "Issues by root cause (all time)", (16, 25, 8, 8),
    [q("/api/v1/ops/root-causes", [("code", "code", "string"), ("count", "count", "number")])],
    fieldConfig={"defaults": {"color": {"mode": "palette-classic"}}, "overrides": []},
    options={"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": True}, "pieType": "donut",
             "legend": {"displayMode": "table", "placement": "right", "values": ["value"]}, "displayLabels": []})

tickets = table("Dispatch tickets (qualified)", (0, 33, 24, 8), "/api/v1/ops/tickets",
    [("status", "Status", "string"), ("ticket_id", "Ticket", "string"), ("source", "Source", "string"),
     ("store_id", "Store", "string"), ("controller_id", "Controller", "string"), ("code", "Root cause", "string"),
     ("required_parts", "Spare parts to bring", "string"), ("wizard_attempts", "Wizard attempts", "number"),
     ("created_at", "Created", "timestamp"), ("note", "Note", "string")], ["Status", "Source"])

history = table("Issue history", (0, 41, 24, 8), "/api/v1/ops/issues",
    [("resolution", "Resolution", "string"), ("state", "State", "string"), ("issue_id", "Issue", "string"), ("store_id", "Store", "string"),
     ("code", "Root cause", "string"), ("opened_at", "Opened", "timestamp"), ("resolved_at", "Resolved", "timestamp"),
     ("duration_min", "Duration (min)", "number"), ("verify_attempts", "Verify attempts", "number")], ["Resolution", "State"])

# ---- Requirement 2: trips & expenses -------------------------------------
def erp_stat(title, field, x, color, unit="none", desc=""):
    p = stat(title, field, x, color, unit, desc)
    p["gridPos"]["y"] = 50
    p["targets"][0]["url"] = "${api}/api/v1/ops/erp/summary"
    return p

erp_row = {"id": 100, "type": "row", "title": "Field service: trips, timesheets & expenses (requirement 2)", "collapsed": False,
           "gridPos": {"x": 0, "y": 49, "w": 24, "h": 1}, "panels": []}
erp_stats = [
    erp_stat("Trips in progress", "trips_active", 0, "blue"),
    erp_stat("Awaiting approval", "trips_awaiting_approval", 4, "orange"),
    erp_stat("Trips approved", "trips_approved", 8, "green"),
    erp_stat("On-site hours", "onsite_hours_total", 12, "purple", unit="h"),
    erp_stat("Travel expenses", "expenses_total_eur", 16, "blue", unit="currencyEUR"),
    erp_stat("Receipts read by AI w/o edits", "receipts_auto_accepted", 20, "green",
             desc="Receipts the vision LLM read with high confidence and nobody had to correct."),
]
erp_trips = table("Trips", (0, 54, 16, 8), "/api/v1/ops/erp/trips",
    [("status", "Status", "string"), ("trip_id", "Trip", "string"), ("engineer", "Engineer", "string"), ("store", "Store", "string"),
     ("code", "Root cause", "string"), ("onsite_hours", "On site (h)", "number"), ("expenses_eur", "Expenses (EUR)", "number"),
     ("receipts", "Receipts", "number"), ("receipts_pending", "To review", "number"), ("geo_warnings", "GPS warnings", "number"),
     ("receipt_envelope_hours", "Receipt envelope (h)", "number")], ["Status"])
TRIP_MAP = {"IN_PROGRESS": {"color": "blue", "index": 20}, "AWAITING_RECEIPT_REVIEW": {"color": "orange", "index": 21},
            "READY_FOR_APPROVAL": {"color": "purple", "index": 22}, "APPROVED": {"color": "green", "index": 23}}
erp_trips["fieldConfig"]["overrides"][0]["properties"][1]["value"] = [{"type": "value", "options": TRIP_MAP}]
erp_trips["fieldConfig"]["overrides"].append({"matcher": {"id": "byName", "options": "Expenses (EUR)"},
                                              "properties": [{"id": "unit", "value": "currencyEUR"}]})
erp_cats = panel("piechart", "Expenses by category", (16, 54, 8, 8),
    [q("/api/v1/ops/erp/expense-categories", [("category", "category", "string"), ("eur", "eur", "number")])],
    fieldConfig={"defaults": {"unit": "currencyEUR", "color": {"mode": "palette-classic"}}, "overrides": []},
    options={"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": True}, "pieType": "donut",
             "legend": {"displayMode": "table", "placement": "right", "values": ["value"]}, "displayLabels": []})

dash = {
    "uid": "rocateq-ops", "title": "Rocateq Operations - Remote Diagnostics", "tags": ["rocateq", "demo"],
    "timezone": "browser", "refresh": "5s", "schemaVersion": 39, "editable": True,
    "time": {"from": "now-30m", "to": "now"},
    "templating": {"list": [{"name": "api", "label": "API base", "type": "constant", "hide": 2,
                             "query": "http://host.docker.internal:8080"}]},
    "panels": panels + [geomap, fleet, volt, loop, issues, pie, tickets, history, erp_row] + erp_stats + [erp_trips, erp_cats],
}
json.dump(dash, open(sys.argv[1], "w", encoding="utf-8"), indent=2, ensure_ascii=False)
print("ok", len(dash["panels"]), "panels")
