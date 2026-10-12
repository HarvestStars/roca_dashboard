"""Generate photo-like sample receipts for the ERP demo.

Usage:  python tools/receipts/gen_receipts.py [--date YYYY-MM-DD] [--out data/sample_receipts]

The trip is a two-day visit Rotterdam -> Berlin (REWE Berlin Mitte) starting on
--date (default: today). Merchants are fictional and every receipt carries a
small "DEMO" footer.

IMPORTANT: this script only renders images. It deliberately writes no JSON /
"expected values" next to them: the application must read every receipt
through the vision LLM, exactly like a real photo.
"""
import argparse
import datetime as dt
import os
import random
import sys

from PIL import Image, ImageDraw, ImageFilter, ImageFont

FONT_DIRS = ["C:/Windows/Fonts", "/usr/share/fonts/truetype/dejavu", "/Library/Fonts", "/System/Library/Fonts/Supplemental"]


def font(names, size):
    for d in FONT_DIRS:
        for n in names:
            p = os.path.join(d, n)
            if os.path.exists(p):
                return ImageFont.truetype(p, size)
    return ImageFont.load_default()


MONO = lambda s: font(["consola.ttf", "cour.ttf", "DejaVuSansMono.ttf", "Courier New.ttf"], s)
MONO_B = lambda s: font(["consolab.ttf", "courbd.ttf", "DejaVuSansMono-Bold.ttf", "Courier New Bold.ttf"], s)
SANS = lambda s: font(["arial.ttf", "DejaVuSans.ttf", "Arial.ttf"], s)
SANS_B = lambda s: font(["arialbd.ttf", "DejaVuSans-Bold.ttf", "Arial Bold.ttf"], s)


def eu(x):
    """German/Dutch number format: 1.234,50"""
    return f"{x:,.2f}".replace(",", "X").replace(".", ",").replace("X", ".")


# ---------------------------------------------------------------- thermal ---
class Thermal:
    """Narrow till receipt, monospace, 42 columns."""

    W, COLS = 560, 34

    def __init__(self):
        self.lines = []  # (text, font, align)

    def add(self, text="", f=None, align="l"):
        self.lines.append((text, f or MONO(22), align))

    def lr(self, left, right, f=None):
        pad = self.COLS - len(left) - len(right)
        self.add(left + " " * max(1, pad) + right, f)

    def rule(self, ch="-"):
        self.add(ch * self.COLS)

    def render(self):
        h = 60 + sum(f.size + 10 for _, f, _ in self.lines) + 40
        img = Image.new("RGB", (self.W, h), (250, 248, 242))
        d = ImageDraw.Draw(img)
        y = 40
        for text, f, align in self.lines:
            w = d.textlength(text, font=f)
            x = {"l": 30, "c": (self.W - w) / 2, "r": self.W - 30 - w}[align]
            d.text((x, y), text, font=f, fill=(35, 35, 40))
            y += f.size + 10
        return img


# --------------------------------------------------------------- invoice ----
class Invoice:
    """A4-ish printed invoice / e-ticket."""

    W, H = 1000, 1340

    def __init__(self):
        self.img = Image.new("RGB", (self.W, self.H), (253, 253, 251))
        self.d = ImageDraw.Draw(self.img)
        self.y = 70

    def text(self, s, f=None, x=70, color=(25, 25, 30), dy=None):
        f = f or SANS(24)
        self.d.text((x, self.y), s, font=f, fill=color)
        self.y += dy if dy is not None else f.size + 14

    def row(self, cols, xs, f=None, color=(25, 25, 30)):
        f = f or SANS(24)
        for s, x in zip(cols, xs):
            if x < 0:  # right aligned at -x
                w = self.d.textlength(s, font=f)
                self.d.text((-x - w, self.y), s, font=f, fill=color)
            else:
                self.d.text((x, self.y), s, font=f, fill=color)
        self.y += f.size + 16

    def hr(self, pad=14):
        self.y += pad // 2
        self.d.line((70, self.y, self.W - 70, self.y), fill=(170, 170, 175), width=2)
        self.y += pad

    def band(self, color, title, sub):
        self.d.rectangle((0, 0, self.W, 150), fill=color)
        self.d.text((70, 38), title, font=SANS_B(46), fill="white")
        self.d.text((72, 98), sub, font=SANS(22), fill=(235, 240, 255))
        self.y = 200


def demo_footer_thermal(r):
    r.add()
    r.add("*** DEMO RECEIPT - not valid ***", MONO(16), "c")


def demo_footer_invoice(inv):
    inv.d.text((70, inv.H - 60), "DEMO DOCUMENT — generated for the Rocateq ERP demo, not a valid invoice", font=SANS(16), fill=(150, 150, 155))


# --------------------------------------------------------------- receipts ---
def breakfast_schiphol(d0):
    r = Thermal()
    r.add("BAKKERIJ DE ZEVEN SLUIZEN", MONO_B(26), "c")
    r.add("Schiphol Plaza, Vertrekhal 2", None, "c")
    r.add("1118 AX Schiphol", None, "c")
    r.add("BTW NL8123.45.678.B01", MONO(18), "c")
    r.rule()
    r.add(f"Datum {d0:%d-%m-%Y}  Tijd 06:25", MONO(22))
    r.add("Kassa 3   Bon 004512", MONO(22))
    r.rule()
    r.lr("1 x Cappuccino groot", eu(4.10))
    r.lr("1 x Croissant roomboter", eu(3.30))
    r.rule()
    r.lr("TOTAAL EUR", eu(7.40), MONO_B(26))
    r.add()
    r.lr("BTW 9%  over", eu(6.79))
    r.lr("BTW 9%", eu(0.61))
    r.add()
    r.lr("PIN (contactloos)", eu(7.40))
    r.add("Bedankt en goede reis!", None, "c")
    demo_footer_thermal(r)
    return r.render()


def flight(d, outbound):
    inv = Invoice()
    inv.band((22, 58, 120), "NORDWIND AIR", "E-ticket receipt  ·  nordwind-air.example")
    fn, frm, to, dep, arr, fare = (("NW 1427", "Amsterdam Schiphol (AMS)", "Berlin Brandenburg (BER)", "07:10", "08:35", 142.00)
                                    if outbound else
                                    ("NW 1432", "Berlin Brandenburg (BER)", "Amsterdam Schiphol (AMS)", "17:45", "19:05", 129.00))
    taxes = 47.00
    inv.row(["Booking reference", "K7Q2XM" if outbound else "K7Q2XN"], [70, 420], SANS_B(24))
    inv.row(["Ticket number", "617-2384419920" if outbound else "617-2384419957"], [70, 420])
    inv.row(["Passenger", "DE BOER / MARK MR"], [70, 420])
    inv.row(["Issued", f"{d - dt.timedelta(days=2):%d %b %Y}"], [70, 420])
    inv.hr()
    inv.text("Itinerary", SANS_B(28))
    inv.row(["Flight", "Date", "From", "Dep", "Arr"], [70, 210, 400, 770, 870], SANS_B(20), (90, 90, 100))
    inv.row([fn, f"{d:%d %b %Y}", frm.split(" (")[0], dep, arr], [70, 210, 400, 770, 870], SANS(22))
    inv.row(["", "", "→ " + to.split(" (")[0], "", ""], [70, 210, 400, 770, 870], SANS(22))
    inv.row(["Class: Economy (Y)   Baggage: 1 x 23 kg   Seat: " + ("14C" if outbound else "09A")], [70], SANS(20), (90, 90, 100))
    inv.hr()
    inv.text("Payment details", SANS_B(28))
    inv.row(["Air fare", f"EUR {fare:.2f}"], [70, -930])
    inv.row(["Airport charges & taxes", f"EUR {taxes:.2f}"], [70, -930])
    inv.row(["of which VAT (international carriage, 0%)", "EUR 0.00"], [70, -930], SANS(20), (90, 90, 100))
    inv.hr()
    inv.row(["TOTAL PAID", f"EUR {fare + taxes:.2f}"], [70, -930], SANS_B(32))
    inv.row(["Paid by Visa ···· 4417", f"{d - dt.timedelta(days=2):%d.%m.%Y}"], [70, -930], SANS(20), (90, 90, 100))
    inv.y += 30
    inv.text("Times are local. Check-in closes 45 minutes before departure.", SANS(18), color=(110, 110, 115))
    demo_footer_invoice(inv)
    return inv.img


def taxi_berlin(d0):
    r = Thermal()
    r.add("TAXI BERLIN", MONO_B(30), "c")
    r.add("Funkzentrale Spree 44 11 22", None, "c")
    r.add("Konz.-Nr. 7731   Fz. B-TX 4711", MONO(18), "c")
    r.rule("=")
    r.add("QUITTUNG", MONO_B(26), "c")
    r.rule("=")
    r.lr("Datum", f"{d0:%d.%m.%Y}")
    r.lr("Beginn", "09:02")
    r.lr("Ende", "09:41")
    r.lr("Strecke", "26,4 km")
    r.add("Von: Flughafen BER T1", MONO(20))
    r.add("Nach: Alexanderstr., 10178 Berlin", MONO(20))
    r.rule()
    r.lr("Fahrpreis", eu(36.50) + " EUR")
    r.lr("Zuschlag Flughafen", eu(2.00) + " EUR")
    r.rule()
    r.lr("GESAMT", eu(38.50) + " EUR", MONO_B(26))
    r.lr("enth. 7% MwSt", eu(2.52) + " EUR")
    r.add()
    r.add("Bezahlt: EC-Karte", MONO(20))
    r.add("Vielen Dank fuer Ihre Fahrt!", None, "c")
    demo_footer_thermal(r)
    return r.render()


def dinner_berlin(d0):
    r = Thermal()
    r.add("Gasthaus Zur Alten Kasse", MONO_B(26), "c")
    r.add("Rosenthaler Str. 9, 10119 Berlin", MONO(20), "c")
    r.add("St.-Nr. 34/567/89012", MONO(18), "c")
    r.rule()
    r.add(f"Rechnung 2-4471   Tisch 12", MONO(22))
    r.add(f"{d0:%d.%m.%Y} 19:42   Bed.: Anja", MONO(22))
    r.rule()
    r.lr("1 Wiener Schnitzel", eu(18.90))
    r.lr("1 Kartoffelsalat", eu(4.90))
    r.lr("1 Apfelschorle 0,4l", eu(4.20))
    r.lr("1 Espresso", eu(2.80))
    r.rule()
    r.lr("SUMME EUR", eu(30.80), MONO_B(28))
    r.add()
    r.lr("Netto 19%", eu(25.88))
    r.lr("MwSt 19%", eu(4.92))
    r.add()
    r.lr("Kartenzahlung Mastercard", eu(30.80))
    r.add("Bewirtungsbeleg auf Rueckseite", MONO(18), "c")
    demo_footer_thermal(r)
    return r.render()


def hotel_berlin(d0):
    d1 = d0 + dt.timedelta(days=1)
    inv = Invoice()
    inv.d.text((70, 60), "Hotel Spreeblick Berlin", font=SANS_B(44), fill=(120, 40, 50))
    inv.d.text((72, 118), "Spreeufer 12 · 10178 Berlin · USt-IdNr. DE298765431", font=SANS(20), fill=(90, 90, 95))
    inv.y = 190
    inv.row(["Rechnung Nr.", "R-2026-08831"], [70, 380], SANS_B(24))
    inv.row(["Rechnungsdatum", f"{d1:%d.%m.%Y}"], [70, 380])
    inv.row(["Gast", "Herr Mark de Boer"], [70, 380])
    inv.row(["Firma", "Rocateq B.V., Rotterdam"], [70, 380])
    inv.row(["Anreise / Abreise", f"{d0:%d.%m.%Y}  –  {d1:%d.%m.%Y}"], [70, 380])
    inv.row(["Zimmer", "412 (Einzelzimmer Superior)"], [70, 380])
    inv.hr()
    inv.row(["Datum", "Leistung", "MwSt", "Betrag EUR"], [70, 250, 690, -930], SANS_B(22), (90, 90, 100))
    inv.row([f"{d0:%d.%m.}", "Übernachtung", "7 %", eu(129.00)], [70, 250, 690, -930])
    inv.row([f"{d0:%d.%m.}", "City Tax Berlin (5 %)", "0 %", eu(6.45)], [70, 250, 690, -930])
    inv.row([f"{d1:%d.%m.}", "Frühstücksbuffet", "19 %", eu(18.00)], [70, 250, 690, -930])
    inv.hr()
    inv.row(["Gesamtbetrag", eu(153.45)], [70, -930], SANS_B(32))
    inv.y += 10
    inv.row(["Netto 7 %", eu(120.56), "MwSt 7 %", eu(8.44)], [70, -500, 600, -930], SANS(22))
    inv.row(["Netto 19 %", eu(15.13), "MwSt 19 %", eu(2.87)], [70, -500, 600, -930], SANS(22))
    inv.row(["MwSt gesamt", "", "", eu(11.31)], [70, -500, 600, -930], SANS_B(22))
    inv.hr()
    inv.row(["Zahlung", f"Visa ···· 4417   {d1:%d.%m.%Y} 07:58"], [70, 380], SANS(22))
    inv.row(["Offener Betrag", "0,00 EUR"], [70, 380], SANS(22))
    inv.y += 30
    inv.text("Vielen Dank für Ihren Aufenthalt. Wir freuen uns auf Ihren nächsten Besuch!", SANS(20), color=(110, 110, 115))
    demo_footer_invoice(inv)
    return inv.img


def parking_faded(d1):
    """Faded thermal paper: deliberately hard to read."""
    r = Thermal()
    r.add("PARKHAUS MITTE", MONO_B(28), "c")
    r.add("Dircksenstr. 40, Berlin", None, "c")
    r.rule()
    r.add("Kassenbeleg", MONO_B(24), "c")
    r.lr("Einfahrt", f"{d1:%d.%m.%y} 08:12")
    r.lr("Ausfahrt", f"{d1:%d.%m.%y} 16:48")
    r.lr("Parkdauer", "8 h 36 min")
    r.rule()
    r.lr("Betrag", eu(24.00) + " EUR", MONO_B(26))
    r.lr("inkl. 19% MwSt", eu(3.83) + " EUR")
    r.add("Danke - Gute Fahrt", None, "c")
    demo_footer_thermal(r)
    img = r.render()
    # thermal fading: lift blacks, low contrast, blur
    img = img.point(lambda v: int(150 + v * 0.42))
    return img.filter(ImageFilter.GaussianBlur(1.6))


# ------------------------------------------------------------ photo effect ---
def photo(paper, seed, bg=(74, 62, 52), angle=None):
    rnd = random.Random(seed)
    angle = rnd.uniform(-4, 4) if angle is None else angle
    pw, ph = paper.size
    W, H = int(pw * 1.25) + 120, int(ph * 1.12) + 120
    # desk background with grain
    bgimg = Image.new("RGB", (W, H), bg)
    noise = Image.effect_noise((W, H), 22).convert("RGB")
    bgimg = Image.blend(bgimg, noise, 0.12)
    # shadow
    mask = Image.new("L", paper.size, 255)
    rot = paper.rotate(angle, expand=True, resample=Image.BICUBIC, fillcolor=bg)
    rmask = mask.rotate(angle, expand=True, resample=Image.BICUBIC)
    shadow = Image.new("RGB", rot.size, (20, 18, 16))
    ox, oy = (W - rot.size[0]) // 2, (H - rot.size[1]) // 2
    smask = rmask.filter(ImageFilter.GaussianBlur(18)).point(lambda v: int(v * 0.55))
    bgimg.paste(shadow, (ox + 14, oy + 18), smask)
    bgimg.paste(rot, (ox, oy), rmask)
    # uneven lighting
    light = Image.radial_gradient("L").resize((W, H)).point(lambda v: 255 - int(v * 0.35))
    bgimg = Image.composite(bgimg, Image.new("RGB", (W, H), (0, 0, 0)), light)
    # sensor noise + slight softness
    bgimg = Image.blend(bgimg, Image.effect_noise((W, H), 30).convert("RGB"), 0.04)
    return bgimg.filter(ImageFilter.GaussianBlur(0.6))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--date", default=dt.date.today().isoformat(), help="first trip day (YYYY-MM-DD)")
    ap.add_argument("--out", default="data/sample_receipts")
    a = ap.parse_args()
    d0 = dt.date.fromisoformat(a.date)
    d1 = d0 + dt.timedelta(days=1)
    os.makedirs(a.out, exist_ok=True)

    items = [
        ("01_breakfast_schiphol.jpg", breakfast_schiphol(d0), (90, 92, 98)),
        ("02_flight_outbound.jpg", flight(d0, True), (64, 70, 78)),
        ("03_taxi_berlin.jpg", taxi_berlin(d0), (74, 62, 52)),
        ("04_dinner_berlin.jpg", dinner_berlin(d0), (52, 46, 40)),
        ("05_hotel_berlin.jpg", hotel_berlin(d0), (80, 84, 88)),
        ("06_parking_faded.jpg", parking_faded(d1), (60, 52, 46)),
        ("07_flight_return.jpg", flight(d1, False), (70, 64, 60)),
    ]
    for i, (name, paper, bg) in enumerate(items):
        img = photo(paper, seed=i, bg=bg)
        img.save(os.path.join(a.out, name), "JPEG", quality=84)
        print("wrote", os.path.join(a.out, name), img.size)


if __name__ == "__main__":
    sys.exit(main())
