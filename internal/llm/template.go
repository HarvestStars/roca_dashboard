package llm

import (
	"context"
	"strings"
)

// Template is the offline provider: hand-written guides per fault and
// language with {location} / {port} placeholders. It is also the fallback
// whenever a remote provider fails, so the store UI never shows an empty guide.
type Template struct{}

func (Template) Name() string { return "template" }

type tplStep struct{ title, text, icon string }

type tpl struct {
	title, fixTime, color string
	steps                 []tplStep
}

func (Template) GenerateGuide(_ context.Context, req GuideRequest) (*Guide, error) {
	lang := req.Language
	byLang, ok := templates[req.FaultCode]
	if !ok {
		byLang = templates["GENERIC"]
	}
	t, ok := byLang[lang]
	if !ok {
		lang = "en"
		t = byLang["en"]
	}
	r := strings.NewReplacer("{location}", nonEmpty(req.ControllerLocation, "?"), "{port}", nonEmpty(req.TargetPort, "?"))
	g := &Guide{Title: t.title, StatusColor: t.color, EstimatedFixTime: t.fixTime, Language: lang, Source: "template"}
	for i, s := range t.steps {
		g.Steps = append(g.Steps, GuideStep{StepNumber: i + 1, Title: r.Replace(s.title), Instruction: r.Replace(s.text), VisualHintIcon: s.icon})
	}
	return g, nil
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

var templates = map[string]map[string]tpl{
	"LOOP_CABLE_DISCONNECTED": {
		"en": {"Cart protection needs a quick check", "2 minutes", "RED", []tplStep{
			{"Find the Rocateq box", "Open the Rocateq control box ({location}).", "cabinet_open"},
			{"Check the yellow cable at port {port}", "Make sure the yellow plug in port {port} is fully inserted. Push it in firmly until you hear a click.", "cable_plug"},
			{"Test the system again", "Press 'Verify solution' below. When the light turns green, the system is working again.", "refresh_check"},
		}},
		"nl": {"Winkelwagenbeveiliging vereist inspectie", "2 minuten", "RED", []tplStep{
			{"Kast lokaliseren", "Open de Rocateq-besturingskast ({location}).", "cabinet_open"},
			{"Gele kabelpoort ({port}) controleren", "Controleer of de gele stekker in poort {port} goed vastzit. Druk deze stevig aan tot u een klik hoort.", "cable_plug"},
			{"Systeem opnieuw testen", "Druk hieronder op 'Opnieuw testen'. Als het lampje groen wordt, werkt het systeem weer.", "refresh_check"},
		}},
		"fr": {"La protection des chariots doit être vérifiée", "2 minutes", "RED", []tplStep{
			{"Trouver le boîtier Rocateq", "Ouvrez le boîtier de commande Rocateq ({location}).", "cabinet_open"},
			{"Vérifier le câble jaune du port {port}", "Vérifiez que la fiche jaune du port {port} est bien enfoncée. Appuyez fermement jusqu'au clic.", "cable_plug"},
			{"Tester à nouveau", "Appuyez sur « Vérifier » ci-dessous. Si le voyant passe au vert, le système fonctionne de nouveau.", "refresh_check"},
		}},
		"de": {"Einkaufswagensicherung muss geprüft werden", "2 Minuten", "RED", []tplStep{
			{"Rocateq-Box finden", "Öffnen Sie den Rocateq-Steuerkasten ({location}).", "cabinet_open"},
			{"Gelbes Kabel an Anschluss {port} prüfen", "Prüfen Sie, ob der gelbe Stecker in Anschluss {port} fest sitzt. Drücken Sie ihn fest hinein, bis er hörbar einrastet.", "cable_plug"},
			{"System erneut testen", "Drücken Sie unten auf „Lösung prüfen“. Wird die Anzeige grün, läuft das System wieder.", "refresh_check"},
		}},
	},
	"POWER_SUPPLY_ABNORMAL": {
		"en": {"The Rocateq box is not getting enough power", "3 minutes", "RED", []tplStep{
			{"Find the power adapter", "Go to the Rocateq control box ({location}) and follow the black power cable to the wall socket.", "cabinet_open"},
			{"Check the plug and power strip", "Push the adapter firmly into the socket. If it is on a power strip, make sure the strip's switch is ON. Do not use an extension shared with heaters or coffee machines.", "power_plug"},
			{"Test the system again", "Press 'Verify solution' below. When the light turns green, the power is back to normal.", "refresh_check"},
		}},
		"nl": {"De Rocateq-kast krijgt te weinig stroom", "3 minuten", "RED", []tplStep{
			{"Stroomadapter zoeken", "Ga naar de Rocateq-besturingskast ({location}) en volg de zwarte stroomkabel naar het stopcontact.", "cabinet_open"},
			{"Stekker en stekkerdoos controleren", "Druk de adapter stevig in het stopcontact. Zit hij op een stekkerdoos, controleer dan of de schakelaar AAN staat. Deel de stekkerdoos niet met kachels of koffiemachines.", "power_plug"},
			{"Systeem opnieuw testen", "Druk hieronder op 'Opnieuw testen'. Als het lampje groen wordt, is de stroomvoorziening weer normaal.", "refresh_check"},
		}},
		"fr": {"Le boîtier Rocateq n'est pas assez alimenté", "3 minutes", "RED", []tplStep{
			{"Trouver l'adaptateur secteur", "Allez au boîtier Rocateq ({location}) et suivez le câble d'alimentation noir jusqu'à la prise murale.", "cabinet_open"},
			{"Vérifier la prise et la multiprise", "Enfoncez bien l'adaptateur dans la prise. S'il est branché sur une multiprise, vérifiez que l'interrupteur est sur MARCHE. Évitez de partager la multiprise avec un chauffage ou une machine à café.", "power_plug"},
			{"Tester à nouveau", "Appuyez sur « Vérifier » ci-dessous. Si le voyant passe au vert, l'alimentation est rétablie.", "refresh_check"},
		}},
		"de": {"Die Rocateq-Box bekommt zu wenig Strom", "3 Minuten", "RED", []tplStep{
			{"Netzteil finden", "Gehen Sie zum Rocateq-Steuerkasten ({location}) und folgen Sie dem schwarzen Stromkabel bis zur Steckdose.", "cabinet_open"},
			{"Stecker und Steckdosenleiste prüfen", "Stecken Sie das Netzteil fest in die Steckdose. Bei einer Steckdosenleiste prüfen, ob der Schalter auf EIN steht. Nicht mit Heizgeräten oder Kaffeemaschinen teilen.", "power_plug"},
			{"System erneut testen", "Drücken Sie unten auf „Lösung prüfen“. Wird die Anzeige grün, ist die Stromversorgung wieder normal.", "refresh_check"},
		}},
	},
	"GATEWAY_OFFLINE": {
		"en": {"We can't reach your Rocateq box", "5 minutes", "RED", []tplStep{
			{"Check the power light", "Look at the Rocateq control box ({location}). Is the small green power light on? If not, check its power plug first.", "power_plug"},
			{"Check the network cable", "Make sure the blue network cable is plugged into the box (port LAN) and into the store router, and that the router is switched on.", "network_cable"},
			{"Test the connection", "Wait one minute, then press 'Verify solution' below.", "refresh_check"},
		}},
		"nl": {"We kunnen uw Rocateq-kast niet bereiken", "5 minuten", "RED", []tplStep{
			{"Stroomlampje controleren", "Kijk naar de Rocateq-besturingskast ({location}). Brandt het kleine groene lampje? Zo niet, controleer dan eerst de stekker.", "power_plug"},
			{"Netwerkkabel controleren", "Controleer of de blauwe netwerkkabel in de kast (poort LAN) en in de router van de winkel zit, en of de router aan staat.", "network_cable"},
			{"Verbinding testen", "Wacht één minuut en druk dan hieronder op 'Opnieuw testen'.", "refresh_check"},
		}},
		"fr": {"Nous ne parvenons pas à joindre votre boîtier Rocateq", "5 minutes", "RED", []tplStep{
			{"Vérifier le voyant d'alimentation", "Regardez le boîtier Rocateq ({location}). Le petit voyant vert est-il allumé ? Sinon, vérifiez d'abord sa prise.", "power_plug"},
			{"Vérifier le câble réseau", "Vérifiez que le câble réseau bleu est branché sur le boîtier (port LAN) et sur le routeur du magasin, et que le routeur est allumé.", "network_cable"},
			{"Tester la connexion", "Attendez une minute, puis appuyez sur « Vérifier » ci-dessous.", "refresh_check"},
		}},
		"de": {"Ihre Rocateq-Box ist nicht erreichbar", "5 Minuten", "RED", []tplStep{
			{"Betriebsanzeige prüfen", "Schauen Sie auf den Rocateq-Steuerkasten ({location}). Leuchtet die kleine grüne Anzeige? Falls nicht, zuerst den Stromstecker prüfen.", "power_plug"},
			{"Netzwerkkabel prüfen", "Prüfen Sie, ob das blaue Netzwerkkabel in der Box (Anschluss LAN) und im Router des Marktes steckt und der Router eingeschaltet ist.", "network_cable"},
			{"Verbindung testen", "Warten Sie eine Minute und drücken Sie dann unten auf „Lösung prüfen“.", "refresh_check"},
		}},
	},
	"ANTENNA_TX_FAULT": {
		"en": {"A part needs replacing — Rocateq is on it", "Technician visit", "ORANGE", []tplStep{
			{"No action needed from you", "An antenna component has failed and cannot be fixed on site by store staff. Please do not open the box.", "info"},
			{"Technician notified", "Rocateq has already been notified with the exact spare part. Other checkouts keep working normally meanwhile.", "technician"},
		}},
		"nl": {"Een onderdeel moet vervangen worden — Rocateq regelt het", "Bezoek monteur", "ORANGE", []tplStep{
			{"U hoeft niets te doen", "Een antenne-onderdeel is defect en kan niet door winkelpersoneel worden gerepareerd. Open de kast a.u.b. niet.", "info"},
			{"Monteur ingelicht", "Rocateq is al op de hoogte, inclusief het juiste reserveonderdeel. De andere kassa's blijven normaal werken.", "technician"},
		}},
		"fr": {"Une pièce doit être remplacée — Rocateq s'en occupe", "Visite technicien", "ORANGE", []tplStep{
			{"Aucune action de votre part", "Un composant d'antenne est défectueux et ne peut pas être réparé par le personnel. Merci de ne pas ouvrir le boîtier.", "info"},
			{"Technicien prévenu", "Rocateq a déjà été prévenu avec la pièce de rechange exacte. Les autres caisses continuent de fonctionner.", "technician"},
		}},
		"de": {"Ein Bauteil muss getauscht werden — Rocateq kümmert sich", "Technikerbesuch", "ORANGE", []tplStep{
			{"Kein Handeln nötig", "Ein Antennenbauteil ist defekt und kann nicht vom Marktpersonal repariert werden. Bitte die Box nicht öffnen.", "info"},
			{"Techniker informiert", "Rocateq wurde bereits mit dem passenden Ersatzteil informiert. Die übrigen Kassen arbeiten normal weiter.", "technician"},
		}},
	},
	"GENERIC": {
		"en": {"Something needs attention", "—", "ORANGE", []tplStep{
			{"Check the Rocateq box", "Look at the Rocateq control box ({location}) for loose cables.", "cabinet_open"},
			{"Test the system again", "Press 'Verify solution'. If it stays red, request technical support.", "refresh_check"},
		}},
	},
}
