import argparse
import json
import re
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

from playwright.sync_api import expect, sync_playwright


def run_checks(base, output):
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        page = browser.new_page(viewport={"width": 1440, "height": 900})
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(base)
        page.wait_for_load_state("networkidle")
        expect(page.locator("#view-goals")).to_be_visible()
        expect(page.locator("#view-goals")).to_have_class(
            re.compile(r"\bis-home\b")
        )
        expect(page.locator("#sidebar")).not_to_be_visible()
        expect(page.locator(".primary-nav button")).to_have_count(3)
        expect(page.locator("#view-hardware")).to_have_count(0)
        assert page.locator(".gleam-wordmark img").evaluate(
            "image => image.complete && image.naturalWidth > 0"
        ), "brand logo did not load"
        page.screenshot(
            path=str(output / "home-light.png"),
            full_page=True,
            animations="disabled",
        )

        page.locator(".quick-card").first.click()
        expect(page.locator("#goal-input")).to_have_value(
            "整理当前工作区的文件，按类型归档并生成一份索引"
        )
        page.locator("#role-picker summary").click()
        page.locator("#expert-bar button").last.click()
        expect(page.locator("#role-picker")).not_to_have_attribute("open", "")
        page.locator("#role-picker summary").click()
        page.locator("#expert-bar button").last.focus()
        page.keyboard.press("Escape")
        expect(page.locator("#role-picker summary")).to_be_focused()
        expect(page.locator("#role-picker")).not_to_have_attribute("open", "")
        page.locator("#goal-input").fill("保留在工作台的草稿")
        page.locator('.primary-nav [data-goto="library"]').click()
        expect(page.locator("#view-library")).to_be_visible()
        expect(page.locator("#view-goals")).not_to_be_visible()
        expect(page.locator(".library-entry")).to_have_count(7)
        page.screenshot(
            path=str(output / "library-light.png"),
            full_page=True,
            animations="disabled",
        )
        for module in [
            "memory",
            "sites",
            "growth",
            "skills",
            "tools",
            "market",
            "geo",
        ]:
            page.locator(f'.library-entry[data-goto="{module}"]').click()
            expect(page.locator(f"#view-{module}")).to_be_visible()
            expect(
                page.locator('.primary-nav [data-goto="library"]')
            ).to_have_attribute("aria-current", "page")
            expect(page.locator("#module-trail")).to_be_visible()
            page.locator('#module-trail [data-goto="library"]').click()
        page.locator('.primary-nav [data-goto="goals"]').click()
        expect(page.locator("#goal-input")).to_have_value("保留在工作台的草稿")

        page.locator("#sidebar-toggle").click()
        expect(page.locator("#sidebar")).to_be_visible()
        expect(page.locator("#convo-new")).to_be_focused()
        page.locator("#history-close").focus()
        page.keyboard.press("Shift+Tab")
        expect(page.locator("#sidebar :focus")).to_have_count(1)
        page.keyboard.press("Tab")
        expect(page.locator("#history-close")).to_be_focused()
        page.keyboard.press("Escape")
        expect(page.locator("#sidebar")).not_to_be_visible()
        expect(page.locator("#sidebar-toggle")).to_be_focused()
        page.locator("#sidebar-toggle").click()
        page.locator("#history-close").click()
        expect(page.locator("#sidebar")).not_to_be_visible()
        page.locator("#global-search").click()
        page.locator("#task-search-input").fill("zzzz-no-result")
        expect(page.locator(".search-empty")).to_contain_text("没有匹配")
        page.locator("#task-search-input").fill("自动化")
        expect(page.locator(".search-row")).to_have_count(1)
        page.locator("#task-search-input").press("Enter")
        expect(page.locator("#view-schedules")).to_be_visible()
        expect(page.locator("#view-goals")).not_to_be_visible()
        page.locator("#sch-new-toggle").click()
        page.locator("#sch-name").fill("shell-regression")
        page.locator("#sch-goal").fill("整理明天的工作清单")
        page.locator("#sch-when").fill("2099-01-01 09:00")
        page.locator("#sch-create").click()
        expect(page.locator("#schedules-list")).to_contain_text(
            "shell-regression"
        )

        page.locator("#sidebar-toggle").click()
        page.locator("#open-me").click()
        page.locator('#me-menu [data-me="settings"]').click()
        expect(page.locator("#view-settings")).to_be_visible()
        expect(page.locator("#view-goals")).not_to_be_visible()
        expect(page.locator("#sidebar")).not_to_be_visible()
        expect(page.locator('.settings-tab[data-stab="index"]')).to_have_count(
            0
        )
        page.locator('.settings-tab[data-stab="conn"]').click()
        expect(page.locator("#s2-conn")).to_contain_text("连接与出网")
        expect(page.locator("#s2-conn")).not_to_contain_text("SSH")
        page.locator('.settings-tab[data-stab="ext"]').click()
        page.locator('#s2-ext [role="tab"]').last.click()
        expect(page.locator("#s2-ext")).to_contain_text("内置角色")
        expect(
            page.locator("#s2-ext button").filter(has_text="添加")
        ).to_have_count(0)
        page.locator('.settings-tab[data-stab="computer"]').click()
        expect(page.locator("#s2-computer")).not_to_contain_text("浏览器操控")
        page.screenshot(
            path=str(output / "settings-light.png"),
            full_page=True,
            animations="disabled",
        )

        page.locator('.primary-nav [data-goto="goals"]').click()
        for width, height in [(1024, 768), (768, 900), (390, 844), (320, 720)]:
            page.set_viewport_size({"width": width, "height": height})
            page.screenshot(
                path=str(output / f"home-{width}.png"),
                full_page=True,
                animations="disabled",
            )
            bounds = page.evaluate("""() => {
                const header = document.querySelector('.topbar');
                const composer = document.querySelector('#composer').getBoundingClientRect();
                return {overflow: header.scrollWidth > header.clientWidth + 1,
                    left: composer.left, right: composer.right, width: innerWidth};
            }""")
            assert not bounds["overflow"], f"header overflows at {width}"
            assert (
                bounds["left"] >= 0 and bounds["right"] <= bounds["width"] + 1
            ), bounds
            page.locator("#goal-submit").scroll_into_view_if_needed()
            toolbar = page.evaluate("""() => {
                const left = document.querySelector('.composer-left').getBoundingClientRect();
                const right = document.querySelector('.composer-right').getBoundingClientRect();
                return {overlap: left.right > right.left && left.bottom > right.top,
                    right: right.right, width: innerWidth};
            }""")
            assert (
                not toolbar["overlap"] and toolbar["right"] <= toolbar["width"]
            ), toolbar
            page.locator("#cp-perm").click()
            expect(page.locator("#cp-perm-pop")).to_be_visible()
            page.locator('#task-seg [data-task="code"]').click()
            expect(page.locator("#role-picker")).not_to_be_visible()
            if not page.locator("#cp-perm-pop").is_visible():
                page.locator("#cp-perm").click()
            page.locator('#task-seg [data-task="work"]').click()
            if page.locator("#cp-perm-pop").is_visible():
                page.locator("#cp-perm").click()
            expect(page.locator("#cp-perm-pop")).not_to_be_visible()
            expect(page.locator("#role-picker")).to_be_visible()
            if width <= 420:
                page.screenshot(
                    path=str(output / f"composer-{width}.png"),
                    full_page=True,
                    animations="disabled",
                )
            page.locator("#sidebar-toggle").click()
            expect(page.locator("#sidebar")).to_be_visible()
            page.locator("#history-close").click()
            page.locator('.primary-nav [data-goto="library"]').click()
            expect(page.locator(".library-entry").first).to_be_visible()
            page.locator('.primary-nav [data-goto="goals"]').click()

        page.set_viewport_size({"width": 1440, "height": 900})
        page.evaluate("UIPrefs.set({themeMode: 'dark'})")
        expect(page.locator("html")).to_have_attribute("data-theme", "dark")
        page.screenshot(
            path=str(output / "home-dark.png"),
            full_page=True,
            animations="disabled",
        )
        page.locator('.primary-nav [data-goto="library"]').click()
        page.screenshot(
            path=str(output / "library-dark.png"),
            full_page=True,
            animations="disabled",
        )
        page.evaluate("UIPrefs.set({themeMode: 'light'})")
        page.locator('.primary-nav [data-goto="goals"]').click()

        page.locator("#sidebar-toggle").click()
        page.locator("#convo-new").click()
        expect(page.locator("#sidebar")).not_to_be_visible()
        expect(page.locator("#view-goals")).to_be_visible()
        expect(page.locator("#view-goals")).to_have_class(
            re.compile(r"\bis-home\b")
        )
        page.locator("#goal-input").fill("请用一句话说明你能做什么")
        page.locator("#goal-input").press("Enter")
        expect(page.locator("#goal-feed")).to_contain_text(
            "请用一句话说明你能做什么"
        )
        page.wait_for_function(
            "() => [...tasks.values()].every(t => t.info.status !== 'running')",
            timeout=30000,
        )
        page.screenshot(
            path=str(output / "conversation.png"),
            full_page=True,
            animations="disabled",
        )
        page.locator('.primary-nav [data-goto="library"]').click()
        page.locator("#sidebar-toggle").click()
        page.locator(".convo-item").first.click()
        expect(page.locator("#view-goals")).to_be_visible()
        expect(page.locator("#sidebar")).not_to_be_visible()
        expect(page.locator("#goal-feed")).to_contain_text(
            "请用一句话说明你能做什么"
        )
        held = []
        page.route(
            "**/api/conversations/*/activate", lambda route: held.append(route)
        )
        page.locator('.primary-nav [data-goto="library"]').click()
        page.locator("#sidebar-toggle").click()
        with page.expect_request("**/api/conversations/*/activate"):
            page.locator(".convo-item").first.click()
        page.locator('.primary-nav [data-goto="goals"]').click()
        assert len(held) == 1
        with page.expect_response("**/api/conversations/*/activate"):
            held[0].continue_()
        page.wait_for_load_state("networkidle")
        expect(page.locator("#view-goals")).not_to_have_class(
            re.compile(r"\bthread-mode\b")
        )
        page.unroute("**/api/conversations/*/activate")

        page.route(
            "**/api/schedules",
            lambda route: route.fulfill(
                status=500, json={"error": "test outage"}
            ),
        )
        page.locator('.primary-nav [data-goto="schedules"]').click()
        expect(page.locator("#schedules-list")).to_contain_text("没能加载")
        page.unroute("**/api/schedules")
        page.locator("#schedules-list").get_by_role(
            "button", name="重试"
        ).click()
        expect(page.locator("#schedules-list")).to_contain_text(
            "shell-regression"
        )

        page.evaluate("applyLang('en')")
        expect(page.locator('.primary-nav [data-goto="library"]')).to_have_text(
            "Library"
        )
        page.locator('.primary-nav [data-goto="library"]').click()
        expect(page.locator(".library-entry")).to_have_count(7)
        page.set_viewport_size({"width": 320, "height": 720})
        assert page.locator(".topbar").evaluate(
            "header => header.scrollWidth <= header.clientWidth + 1"
        ), "English navigation overflows"
        page.evaluate("applyLang('zh')")
        page.set_viewport_size({"width": 1440, "height": 900})
        page.reload()
        page.wait_for_load_state("networkidle")
        expect(page.locator("#sidebar")).not_to_be_visible()
        assert not errors, errors
        browser.close()
        print(
            "PASS: navigation, modules, drafts, history, search, schedules, settings, responsive layout, themes and conversations"
        )


def new_test_page(browser, base):
    page = browser.new_page(viewport={"width": 1440, "height": 900})
    page.goto(base)
    page.wait_for_load_state("networkidle")
    return page


def run_terminal_checks(base, output):
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        page = new_test_page(browser, base)
        other = new_test_page(browser, base)
        held = []
        page.route("**/api/tools/call", lambda route: held.append(route))
        page.locator("#term-toggle").click()
        page.locator("#term-input").fill("cd .")
        with page.expect_request("**/api/tools/call"):
            page.locator("#term-input").press("Enter")
        expect(page.locator("#term-input")).to_be_disabled()
        other.evaluate("""() => {
            window.otherCall = api('POST', '/api/tools/call', {
                name: 'shell.exec', args: {command: 'echo OTHER_TAB'}
            });
        }""")
        foreign = page.locator(".standalone-approval")
        expect(foreign).to_be_visible()
        expect(page.locator(".term-approval")).to_have_count(0)
        foreign.get_by_role("button", name="批准执行").click()
        result = other.evaluate("() => window.otherCall")
        assert "OTHER_TAB" in result["output"]["stdout"], result
        expect(page.locator("#term-input")).to_be_disabled()
        request = held[0].request.post_data_json
        assert request.get("request_id"), request
        held[0].continue_()
        own = page.locator(".term-approval")
        expect(own).to_be_visible()
        plan = page.evaluate(
            """async requestID => {
            const {approvals} = await api('GET', '/api/approvals');
            return approvals.find(ap => ap.request_id === requestID);
        }""",
            request["request_id"],
        )
        assert plan, "direct call approval lost request_id"
        for line in plan["plan"]:
            expect(own).to_contain_text(line)
        tool, raw = plan["plan"][0].split(": ", 1)
        shown = json.loads(raw)
        assert tool == "shell.exec", plan
        assert shown["command"] == request["args"]["command"], plan
        assert shown["cwd"] == request["args"]["cwd"], plan
        expect(
            other.locator(".standalone-approval button").filter(
                has_text="批准执行"
            )
        ).to_have_count(1)
        page.screenshot(path=str(output / "terminal-own-approval.png"))
        own.get_by_role("button", name="批准执行").click()
        expect(page.locator("#term-input")).to_be_enabled()
        expect(page.locator(".term-approval")).to_have_count(0)
        page.unroute("**/api/tools/call")
        with page.expect_request("**/api/tools/call") as second:
            page.locator("#term-input").fill("echo SECOND_CALL")
            page.locator("#term-input").press("Enter")
        assert (
            second.value.post_data_json["request_id"] != request["request_id"]
        )
        expect(own).to_be_visible()
        own.get_by_role("button", name="拒绝", exact=True).click()
        expect(page.locator("#term-input")).to_be_enabled()
        expect(page.locator("#term-log")).to_contain_text("操作被拒绝")
        browser.close()
        print(
            "PASS: terminal request correlation, cross-tab global approval, actual plan and command, unique IDs, denial"
        )


def create_test_conversation(page, title):
    return page.evaluate(
        """title => api('POST', '/api/conversations', {
        title, space_id: 'default'
    })""",
        title,
    )


def select_test_conversation(page, conversation):
    page.evaluate("() => loadConvoList()")
    page.locator("#sidebar-toggle").click()
    page.locator(f'.convo-item[data-convo-id="{conversation["id"]}"]').click()
    page.wait_for_load_state("networkidle")
    expect(page.locator("#sidebar")).not_to_be_visible()


def hold_goal_response(page):
    held = []
    page.evaluate("() => { window.heldGoalReady = false; }")

    def intercept(route):
        if route.request.method != "POST":
            route.continue_()
            return
        response = route.fetch()
        held.append((route, response, response.json()))
        page.evaluate("() => { window.heldGoalReady = true; }")

    page.route("**/api/goals", intercept)
    return held


def submit_held_goal(page, held, text):
    page.locator("#goal-input").fill(text)
    with page.expect_request("**/api/goals"):
        page.locator("#goal-submit").click()
    expect(page.locator("#goal-submit")).to_be_disabled()
    page.wait_for_function("() => window.heldGoalReady")
    assert len(held) == 1, "goal response was not captured"
    return held[0][2]["task_id"]


def release_goal_response(page, held):
    held[0][0].fulfill(response=held[0][1])
    expect(page.locator("#goal-submit")).to_be_enabled()
    page.unroute("**/api/goals")


def run_submit_checks(base, output):
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        page = new_test_page(browser, base)
        a = create_test_conversation(page, "Submit A")
        b = create_test_conversation(page, "Submit B")
        select_test_conversation(page, a)
        held = hold_goal_response(page)
        text = "This submitted message belongs only to conversation A"
        task_id = submit_held_goal(page, held, text)
        page.evaluate("""() => {
            window.__log = [];
            const origCard = renderTaskCard;
            window.renderTaskCard = function (info, prepend) {
                window.__log.push({fn: 'renderTaskCard', view: viewingConvo,
                    convo: currentConvo && currentConvo.id,
                    taskConvo: info.conversation_id, id: info.task_id,
                    stack: (new Error()).stack.split('\\n').slice(1, 5).join(' | ')});
                return origCard(info, prepend);
            };
            const origLoad = loadGoals;
            window.loadGoals = function () {
                window.__log.push({fn: 'loadGoals', view: viewingConvo,
                    convo: currentConvo && currentConvo.id,
                    stack: (new Error()).stack.split('\\n').slice(1, 5).join(' | ')});
                return origLoad();
            };
        }""")
        select_test_conversation(page, b)
        page.locator("#goal-input").fill("B unsent draft")
        page.evaluate(
            "() => { clearRefs(); addRef('memory', 'B ref', 'B context'); }"
        )
        release_goal_response(page, held)
        expect(page.locator("#goal-input")).to_have_value("B unsent draft")
        expect(page.locator("#composer-refs")).to_contain_text("B ref")
        expect(page.locator("#goal-feed")).not_to_contain_text(text)
        detail = page.evaluate("id => api('GET', '/api/goals/' + id)", task_id)
        assert detail["conversation_id"] == a["id"], detail
        select_test_conversation(page, a)
        expect(page.locator("#goal-feed")).to_contain_text(text)
        page.screenshot(path=str(output / "submit-conversation-recovered.png"))
        browser.close()
        print(
            "PASS: late submit response preserves other conversation, draft and refs; submitted conversation restores from backend"
        )


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--output", default="tmp/ui-shell-check")
    parser.add_argument(
        "--suite", choices=["all", "shell", "terminal", "submit"], default="all"
    )
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="gleam-shell-") as directory:
        root = Path(directory)
        workspace = root / "workspace"
        workspace.mkdir()
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        base = f"http://127.0.0.1:{port}"
        with (output / "server.log").open("w", encoding="utf-8") as log:
            server = subprocess.Popen(
                [
                    binary,
                    "webui",
                    "--addr",
                    f"127.0.0.1:{port}",
                    "--workspace",
                    str(workspace),
                    "--data-dir",
                    str(root / "data"),
                    "--mock-llm",
                ],
                stdout=log,
                stderr=log,
            )
            try:
                for _ in range(100):
                    if server.poll() is not None:
                        raise RuntimeError(
                            "test server exited before becoming ready"
                        )
                    try:
                        with urllib.request.urlopen(base, timeout=1):
                            break
                    except (urllib.error.URLError, TimeoutError):
                        time.sleep(0.1)
                else:
                    raise RuntimeError("test server did not become ready")
                print(f"Isolated mock server: {base}", flush=True)
                if args.suite in ("all", "shell"):
                    run_checks(base, output)
                if args.suite in ("all", "terminal"):
                    run_terminal_checks(base, output)
                if args.suite in ("all", "submit"):
                    run_submit_checks(base, output)
            finally:
                server.terminate()
                server.wait(timeout=10)


if __name__ == "__main__":
    main()
