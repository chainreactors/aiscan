"""Local business fixtures and independent Playwright/native-command comparison.

The control API requires an unguessable key which is never included in agent
prompts. Receipts exist only in server memory until the required effects finish.
Run --serve for the Go JEV acceptance suite, or --driver PATH --out REPORT.
"""
import argparse
import hashlib
import json
import os
import subprocess
import sys
import tempfile
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse


KINDS = ["expense", "shadow", "repeat", "popup", "frame", "files", "drag"]


class Lab:
    def __init__(self):
        self.key = uuid.uuid4().hex
        self.cases = {}
        self.lock = threading.RLock()
        lab = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                self.handle_request()

            def do_POST(self):
                self.handle_request()

            def respond(self, status, body, mime="application/json", headers=None):
                if not isinstance(body, bytes):
                    body = (json.dumps(body) if mime == "application/json" else body).encode()
                self.send_response(status)
                self.send_header("Content-Type", mime)
                self.send_header("Content-Length", str(len(body)))
                for name, value in (headers or {}).items():
                    self.send_header(name, value)
                self.end_headers()
                self.wfile.write(body)

            def handle_request(self):
                path = urlparse(self.path).path
                raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
                if path.startswith("/__control__/"):
                    if self.headers.get("X-Lab-Key") != lab.key:
                        self.respond(403, {"error": "private test oracle"})
                        return
                    args = json.loads(raw or b"{}")
                    with lab.lock:
                        if path.endswith("/new"):
                            self.respond(200, lab.new_case(**args))
                        elif path.endswith("/check"):
                            self.respond(200, lab.check(args["id"], args.get("output", "")))
                        else:
                            self.respond(404, {})
                    return
                parts = path.strip("/").split("/")
                if len(parts) < 2 or parts[0] != "case" or parts[1] not in lab.cases:
                    self.respond(404, {})
                    return
                with lab.lock:
                    c = lab.cases[parts[1]]
                    endpoint = parts[2] if len(parts) > 2 else ""
                    if endpoint in ("", "idp", "frame") and self.command == "GET":
                        self.respond(200, lab.page(c, endpoint), "text/html; charset=utf-8")
                        return
                    if endpoint == "download" and self.command == "GET":
                        c["downloads"] += 1
                        if c["effects"] != 1:
                            c["wrong"] += 1
                            self.respond(409, {})
                        else:
                            self.respond(200, c["download_bytes"], "application/octet-stream",
                                         {"Content-Disposition": 'attachment; filename="export.csv"'})
                        return
                    if endpoint == "status" and self.command == "GET":
                        c["polls"] += 1
                        if not c["effects"]:
                            c["wrong"] += 1
                            self.respond(409, {})
                        elif c["polls"] == 1:
                            self.respond(503, {"state": "temporary outage"})
                        elif c["polls"] < 3:
                            self.respond(200, {"state": "pending"})
                        else:
                            self.respond(200, {"receipt": c["receipt"]})
                        return
                    if self.command != "POST" or endpoint != "act":
                        c["wrong"] += 1
                        self.respond(404, {})
                        return
                    data = json.loads(raw or b"{}")
                    c["events"].append(data)
                    kind = c["kind"]
                    valid = data.get("trusted") is True
                    if kind == "expense":
                        valid = valid and c["effects"] == 0 and data.get("employee") == c["employee"] and data.get("reference") == c["reference"] and data.get("amount") == c["amount"] and data.get("area") == c["area"]
                    elif kind == "shadow":
                        valid = valid and c["effects"] == 0 and data.get("reference") == c["reference"]
                    elif kind == "repeat":
                        valid = valid and ((data.get("op") == "add" and c["effects"] < 2) or (data.get("op") == "checkout" and c["effects"] == 2))
                    elif kind == "popup":
                        valid = valid and c["effects"] == 0 and data.get("employee") == c["employee"]
                    elif kind == "frame":
                        valid = valid and c["effects"] == 0 and data.get("amount") == c["amount"]
                    elif kind == "files":
                        valid = valid and c["effects"] == 0 and data.get("contents") == c["upload_bytes"].decode()
                    elif kind == "drag":
                        valid = valid and c["effects"] == 0 and data.get("item") == c["item"]
                    if not valid:
                        c["wrong"] += 1
                        self.respond(409, {"error": "wrong, repeated, untrusted, or incomplete effect"})
                        return
                    c["effects"] += 1
                    if kind == "expense":
                        # Commit succeeded but the client did not receive a success response.
                        self.respond(503, {"error": "response lost after commit; inspect status"})
                    elif kind == "repeat" and data["op"] == "add":
                        self.respond(200, {"quantity": c["effects"]})
                    else:
                        self.respond(200, {"receipt": c["receipt"]})

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self.server.server_port}"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def new_case(self, kind, index=0, artifact_dir=None):
        if kind not in KINDS:
            raise ValueError(kind)
        ident = uuid.uuid4().hex
        directory = Path(artifact_dir or tempfile.mkdtemp(prefix="jev-browser-lab-"))
        directory.mkdir(parents=True, exist_ok=True)
        c = dict(id=ident, kind=kind, index=index, effects=0, wrong=0, polls=0, downloads=0, events=[],
                 receipt="receipt-" + uuid.uuid4().hex, employee=f"employee-{index}-{ident[:5]}",
                 reference=f"报销 '{index}' \\ reference {ident[:5]}", amount=str(101 + index),
                 area=["North", "South"][index % 2], item=137 + index,
                 upload_bytes=f"employee,reference\n{ident},{index}\n".encode(),
                 download_bytes=f"validated-export,{ident},{uuid.uuid4().hex}\n".encode(),
                 upload_path=str(directory / f"input-{ident}.csv"),
                 download_path=str(directory / f"download-{ident}.csv"))
        Path(c["upload_path"]).write_bytes(c["upload_bytes"])
        self.cases[ident] = c
        goals = {
            "expense": f"Create exactly one expense for employee {c['employee']}, amount {c['amount']}, area {c['area']}, reference {json.dumps(c['reference'], ensure_ascii=False)}. Submit once. If the response fails, inspect the existing request status rather than creating another request. Wait through transient status failures and report the final displayed receipt.",
            "shadow": f"In the CRM custom component, set Customer reference to {json.dumps(c['reference'], ensure_ascii=False)} and Save customer once. The outside Delete customer button is unrelated. Report the displayed receipt.",
            "repeat": "Add exactly two units of the item using the same Add one button twice, then Checkout once. Report the displayed receipt. Two distinct add operations are required.",
            "popup": f"Sign in through the identity-provider popup as {c['employee']}, approve once, return to the original tab and report its displayed receipt. Do not create a separate login tab or navigate directly to a callback.",
            "frame": f"In the embedded payment workspace, enter amount {c['amount']} and Confirm payment once. Report the receipt displayed in the parent page. Keep the iframe embedded.",
            "files": f"Upload the supplied CSV {c['upload_path']} using the browser file control, Validate export once, download the resulting file through its download link and save it as {c['download_path']}. Report the displayed receipt and the saved file SHA-256. A fetch body or suggested path without an actual saved file is insufficient.",
            "drag": f"Find item {c['item']} in the virtual inventory list, then drag its card onto Approved. This requires browser pointer input; synthetic DOM events are not accepted. Report the displayed receipt.",
        }
        prompt = goals[kind]
        if kind in ("expense", "shadow"):
            prompt += " Quoted field values above are JSON-encoded strings: decode each exactly once before entering it, preserving its decoded literal quotes and backslashes."
        return {k: c[k] for k in ("id", "kind", "employee", "reference", "amount", "area", "item", "upload_path", "download_path")} | {"url": f"{self.url}/case/{ident}/", "prompt": prompt}

    def check(self, ident, output):
        c = self.cases[ident]
        expected = 3 if c["kind"] == "repeat" else 1
        artifact_ok = True
        if c["kind"] == "files":
            p = Path(c["download_path"])
            artifact_ok = p.exists() and p.read_bytes() == c["download_bytes"] and hashlib.sha256(c["download_bytes"]).hexdigest() in output and c["downloads"] == 1
        complete = c["effects"] == expected and c["wrong"] == 0 and c["receipt"] in output and artifact_ok
        return dict(correct=complete, effects=c["effects"], expected_effects=expected, wrong=c["wrong"], polls=c["polls"], downloads=c["downloads"], artifact_ok=artifact_ok, events=c["events"], receipt_observed=c["receipt"] in output)

    def page(self, c, endpoint):
        # Parameters are visible task data, never expected receipts or oracle state.
        base = f"/case/{c['id']}/"
        cross = f"http://localhost:{self.server.server_port}{base}"
        js = """const root=document.querySelector('main'),out=document.querySelector('output');
async function act(data,e){data.trusted=!!e.isTrusted;let r=await fetch(BASE+'act',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});let d=await r.json();if(d.receipt)out.textContent=d.receipt;return d}
function button(text,fn,parent=root){const b=document.createElement('button');b.textContent=text;b.id='node-'+crypto.randomUUID();b.onclick=fn;parent.append(b);return b}
function field(label,parent=root){const l=document.createElement('label'),i=document.createElement('input');l.textContent=label;i.id='field-'+crypto.randomUUID();l.append(i);parent.append(l);return i}
""".replace("BASE", json.dumps(base))
        kind = c["kind"]
        if endpoint == "idp":
            body = "const employee=field('Employee');button('Approve sign in',async e=>{let d=await act({employee:employee.value},e);if(d.receipt){opener.postMessage(d,'*');window.close()}});"
        elif endpoint == "frame":
            body = "const amount=field('Amount');button('Confirm payment',async e=>{let d=await act({amount:amount.value},e);if(d.receipt)parent.postMessage(d,'*')});"
        elif kind == "expense":
            body = """const employee=field('Employee'),amount=field('Amount'),reference=field('Reference');
const label=document.createElement('label'),area=document.createElement('select');label.textContent='Area';area.id='area-'+crypto.randomUUID();label.htmlFor=area.id;root.append(label,area);for(const text of ['Choose area','North','South']){let o=document.createElement('option');o.textContent=text;area.append(o)}
let submit;area.onchange=()=>{if(submit)submit.remove();out.textContent='Validating cost center';setTimeout(()=>{out.textContent='';submit=button('Submit expense',async e=>{submit.disabled=true;await act({employee:employee.value,amount:amount.value,reference:reference.value,area:area.value},e);out.textContent='Submission response unavailable; checking request status';async function poll(){let r=await fetch(BASE+'status');let d=await r.json();if(d.receipt)out.textContent=d.receipt;else setTimeout(poll,140)}poll()})},180)};
button('Cancel expense',e=>act({cancel:true},e));""".replace("BASE", json.dumps(base))
        elif kind == "shadow":
            body = "const host=document.createElement('customer-editor');root.append(host);const shadow=host.attachShadow({mode:'open'}),reference=field('Customer reference',shadow);button('Save customer',e=>act({reference:reference.value},e),shadow);button('Delete customer',e=>act({delete:true},e));"
        elif kind == "repeat":
            body = "button('Add one',async e=>{let d=await act({op:'add'},e);if(d.quantity)document.querySelector('#quantity').textContent=d.quantity});const q=document.createElement('p');q.id='quantity';q.textContent='0';root.append(q);button('Checkout',e=>act({op:'checkout'},e));"
        elif kind == "popup":
            body = f"button('Sign in',()=>window.open({json.dumps(cross+'idp')},'identity-provider','width=600,height=400'));window.addEventListener('message',e=>{{if(e.origin==={json.dumps(cross.rstrip('/').split('/case/')[0])}&&e.data.receipt)out.textContent=e.data.receipt}});"
        elif kind == "frame":
            body = f"const frame=document.createElement('iframe');frame.title='Payment workspace';frame.src={json.dumps(cross+'frame')};frame.style='width:700px;height:300px';root.append(frame);window.addEventListener('message',e=>{{if(e.source===frame.contentWindow&&e.data.receipt)out.textContent=e.data.receipt}});"
        elif kind == "files":
            body = f"const file=field('CSV file');file.type='file';button('Validate export',async e=>{{let d=await act({{contents:file.files.length?await file.files[0].text():''}},e);if(d.receipt){{let a=document.createElement('a');a.textContent='Download export';a.href={json.dumps(base+'download')};a.download='export.csv';root.append(a)}}}});"
        else:
            body = """const list=document.createElement('section');list.setAttribute('aria-label','Virtual inventory');list.style='height:180px;overflow:auto;width:360px';const space=document.createElement('div');space.style='height:6000px;position:relative';list.append(space);root.append(list);
function render(){space.replaceChildren();let start=Math.floor(list.scrollTop/30);for(let n=start;n<Math.min(200,start+7);n++){let card=document.createElement('div');card.textContent='item '+n;card.draggable=true;card.style='position:absolute;top:'+(n*30)+'px;height:28px;width:300px;background:#ddd';card.ondragstart=e=>e.dataTransfer.setData('text/plain',String(n));space.append(card)}}list.onscroll=render;render();
const target=document.createElement('section');target.textContent='Approved';target.setAttribute('aria-label','Approved');target.style='padding:40px;background:#ddd;margin:20px;width:200px';target.ondragover=e=>e.preventDefault();target.ondrop=e=>{e.preventDefault();act({item:Number(e.dataTransfer.getData('text/plain'))},e)};root.append(target);"""
        return f"<!doctype html><meta charset='utf-8'><title>{kind} workspace</title><style>button,label,input,select{{display:block;margin:12px}}body{{font:16px sans-serif}}</style><h1>{kind} workspace</h1><main></main><output></output><script>{js}{body}</script>"


class NativeDriver:
    def __init__(self, path):
        self.proc = subprocess.Popen([path], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                     stderr=subprocess.PIPE, text=True, encoding="utf-8")
        self.calls = []

    def run(self, *args):
        self.proc.stdin.write(json.dumps({"args": args}) + "\n")
        self.proc.stdin.flush()
        line = self.proc.stdout.readline()
        if not line:
            raise RuntimeError("native driver exited unexpectedly")
        r = json.loads(line)
        self.calls.append({"args": args, **r})
        if r.get("error"):
            raise RuntimeError(r["error"])
        return r.get("output", "")

    def close(self):
        try:
            self.run("__quit__")
        except (RuntimeError, BrokenPipeError):
            pass
        try:
            self.proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.proc.kill()


def official(lab, c, browser):
    context = browser.new_context(accept_downloads=True)
    page = context.new_page()
    page.set_default_timeout(6000)
    try:
        page.goto(c["url"])
        kind = c["kind"]
        if kind == "expense":
            for label, key in [("Employee", "employee"), ("Amount", "amount"), ("Reference", "reference")]:
                page.get_by_label(label, exact=True).fill(c[key])
            page.get_by_label("Area", exact=True).select_option(label=c["area"])
            page.get_by_role("button", name="Submit expense", exact=True).click()
        elif kind == "shadow":
            page.get_by_label("Customer reference", exact=True).fill(c["reference"])
            page.get_by_role("button", name="Save customer", exact=True).click()
        elif kind == "repeat":
            for n in range(1, 3):
                page.get_by_role("button", name="Add one", exact=True).click()
                page.wait_for_function("n=>document.querySelector('#quantity').textContent===String(n)", arg=n)
            page.get_by_role("button", name="Checkout", exact=True).click()
        elif kind == "popup":
            with page.expect_popup() as opened:
                page.get_by_role("button", name="Sign in", exact=True).click()
            popup = opened.value
            popup.get_by_label("Employee", exact=True).fill(c["employee"])
            popup.get_by_role("button", name="Approve sign in", exact=True).click()
        elif kind == "frame":
            frame = page.frame_locator("iframe[title='Payment workspace']")
            frame.get_by_label("Amount", exact=True).fill(c["amount"])
            frame.get_by_role("button", name="Confirm payment", exact=True).click()
        elif kind == "files":
            page.get_by_label("CSV file", exact=True).set_input_files(c["upload_path"])
            page.get_by_role("button", name="Validate export", exact=True).click()
            with page.expect_download() as event:
                page.get_by_role("link", name="Download export", exact=True).click()
            event.value.save_as(c["download_path"])
        elif kind == "drag":
            page.get_by_label("Virtual inventory").evaluate("(el,n)=>el.scrollTop=n*30", c["item"])
            page.get_by_text(f"item {c['item']}", exact=True).drag_to(page.get_by_label("Approved", exact=True))
        page.wait_for_function("document.querySelector('output').textContent.startsWith('receipt-')")
        output = page.locator("output").inner_text()
        if kind == "files":
            output += " " + hashlib.sha256(Path(c["download_path"]).read_bytes()).hexdigest()
        return output
    finally:
        context.close()


def native(lab, c, driver):
    s = "probe-" + c["id"][:8]
    run = lambda *args: driver.run(args[0], s, *args[1:])
    driver.run("open", c["url"], "--session", s, "--op-timeout", "2", "--no-speed-up")
    try:
        kind = c["kind"]
        if kind == "expense":
            for label, key in [("Employee", "employee"), ("Amount", "amount"), ("Reference", "reference")]:
                run("fill", "label=" + label, c[key])
            run("select-option", "label=Area", c["area"])
            run("wait-for", 'role=button[name="Submit expense"]')
            run("click", 'role=button[name="Submit expense"]')
        elif kind == "shadow":
            run("fill", "label=Customer reference", c["reference"])
            run("click", 'role=button[name="Save customer"]')
        elif kind == "repeat":
            run("click", 'role=button[name="Add one"]')
            run("click", 'role=button[name="Add one"]')
            run("click", 'role=button[name="Checkout"]')
        elif kind == "popup":
            run("click", 'role=button[name="Sign in"]')
            listing = run("tab-list")
            if "Tabs (1)" in listing:
                raise RuntimeError("website popup absent from session tab-list: " + listing)
            run("tab-select", "1")
            run("fill", "label=Employee", c["employee"])
            run("click", 'role=button[name="Approve sign in"]')
            run("tab-select", "0")
        elif kind == "frame":
            inspection = run("evaluate", "({frames:document.querySelectorAll('iframe').length,childDocument:!!document.querySelector('iframe').contentDocument})")
            try:
                run("fill", "label=Amount", c["amount"])
            except RuntimeError as exc:
                raise RuntimeError("no frame-scoped command; parent selector cannot reach cross-origin frame: " + inspection + "; " + str(exc)) from exc
            run("click", 'role=button[name="Confirm payment"]')
        elif kind == "files":
            run("set-input-files", "label=CSV file", c["upload_path"])
            run("click", 'role=button[name="Validate export"]')
            run("wait-for", 'role=link[name="Download export"]')
            run("click", 'role=link[name="Download export"]')
            raise RuntimeError("no native download event/save command; file was not saved to requested path")
        elif kind == "drag":
            run("evaluate", "document.querySelector('[aria-label=\"Virtual inventory\"]').scrollTop=" + str(c["item"] * 30))
            run("drag", "text=item " + str(c["item"]), "[aria-label=Approved]")
        run("wait-for", "output:not(:empty)")
        # Status text is not a receipt: wait for its actual final value.
        deadline = time.monotonic() + 6
        while time.monotonic() < deadline:
            text = run("inner-text", "output")
            if "receipt-" in text:
                return text
        raise RuntimeError("no observed receipt")
    finally:
        driver.run("close", s)


def compare(args):
    from playwright.sync_api import sync_playwright
    lab, rows = Lab(), []
    driver = NativeDriver(str(Path(args.driver).resolve()))
    try:
        with sync_playwright() as p:
            options = {"headless": True}
            if args.browser:
                options["executable_path"] = args.browser
            browser = p.chromium.launch(**options)
            for index, kind in enumerate(KINDS):
                for engine in ("official_playwright", "native_command"):
                    c = lab.new_case(kind, index, str(Path(args.out).resolve().parent / "artifacts" / kind / engine))
                    before = len(driver.calls)
                    started = time.monotonic()
                    output, error = "", ""
                    try:
                        output = official(lab, c, browser) if engine == "official_playwright" else native(lab, c, driver)
                    except Exception as exc:
                        error = str(exc).split("\n\nplaywright - Headless")[0]
                    row = dict(kind=kind, engine=engine, ms=round((time.monotonic()-started)*1000), error=error, output=output,
                               oracle=lab.check(c["id"], output), calls=driver.calls[before:] if engine == "native_command" else [])
                    rows.append(row)
                    print(json.dumps({"kind": kind, "engine": engine, "correct": row["oracle"]["correct"], "error": error}, ensure_ascii=False), flush=True)
                    Path(args.out).write_text(json.dumps({"real_browser": True, "real_models": False, "rows": rows}, ensure_ascii=False, indent=2), encoding="utf-8")
            browser.close()
    finally:
        driver.close()
        lab.server.shutdown()
    if any(not r["oracle"]["correct"] for r in rows if r["engine"] == "official_playwright"):
        raise SystemExit("official baseline failed; fixture must be investigated")


def template_probe(args):
    # The native template engine also has a drag action. Check actual HTML5
    # drop completion separately from the CLI's missing direct drag command.
    lab = Lab()
    driver = NativeDriver(str(Path(args.driver).resolve()))
    c = lab.new_case("drag", 0, str(Path(args.out).resolve().parent / "template-drag"))
    template = {
        "id": "browser-lab-html5-drag", "info": {"name": "HTML5 drag validation", "author": "cyber", "severity": "info"},
        "headless": [{"steps": [
            {"action": "navigate", "args": {"url": "{{BaseURL}}"}},
            {"action": "script", "args": {"code": "() => {document.querySelector('[aria-label=\\\"Virtual inventory\\\"]').scrollTop=" + str(c["item"] * 30) + ";}"}},
            {"action": "waitvisible", "args": {"by": "text", "text": "item " + str(c["item"]), "timeout": "3"}},
            {"action": "drag", "args": {"by": "text", "text": "item " + str(c["item"]), "target": "[aria-label=Approved]"}},
            {"action": "waitvisible", "args": {"selector": "output:not(:empty)", "timeout": "3"}},
            {"action": "script", "name": "receipt", "args": {"code": "() => document.querySelector('output').textContent"}}
        ], "matchers": [{"type": "word", "part": "receipt", "words": ["receipt-"]}],
            "extractors": [{"type": "regex", "part": "receipt", "regex": ["receipt-[a-f0-9]+"]}]}]
    }
    path = Path(c["upload_path"]).parent / "drag-template.yaml"
    path.write_text(json.dumps(template), encoding="utf-8")
    output, error = "", ""
    try:
        output = driver.run("template", str(path), c["url"])
    except Exception as exc:
        error = str(exc)
    finally:
        driver.close()
        lab.server.shutdown()
    result = dict(engine="native_template", kind="drag", output=output, error=error, oracle=lab.check(c["id"], output), calls=driver.calls)
    Path(args.out).write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(result, ensure_ascii=False), flush=True)


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    parser = argparse.ArgumentParser()
    parser.add_argument("--serve", action="store_true")
    parser.add_argument("--template-probe", action="store_true")
    parser.add_argument("--driver")
    parser.add_argument("--browser", default=os.environ.get("CYBER_BROWSER_PATH", ""))
    parser.add_argument("--out")
    args = parser.parse_args()
    if args.serve:
        lab = Lab()
        print(json.dumps({"url": lab.url, "key": lab.key}), flush=True)
        try:
            for _ in sys.stdin:
                pass
        finally:
            lab.server.shutdown()
    elif args.template_probe:
        template_probe(args)
    else:
        compare(args)
