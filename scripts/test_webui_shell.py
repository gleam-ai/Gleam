import argparse
import json
import re
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from contextlib import contextmanager
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


def run_home_feed_checks(base, output):
    conversations = [
        {"id": "c-run", "title": "正在跑的归档", "space_id": "default"},
        {"id": "c-fail", "title": "失败的部署", "space_id": "default"},
        {"id": "c-ok", "title": "完成的归档", "space_id": "default"},
        {"id": "c-stop", "title": "停下的清理", "space_id": "default"},
        {"id": "c-wait", "title": "等待批准的发布", "space_id": "default"},
        {"id": "c-part", "title": "半途而废的巡检", "space_id": "default"},
        {"id": "c-idle", "title": "还没有任务的对话", "space_id": "default"},
    ]
    failed = {
        "task_id": "fail0001",
        "goal": "失败的部署任务",
        "conversation_id": "c-fail",
        "status": "failed",
        "mode": "auto",
        "task_mode": "execute",
        "started_at": "2026-10-26T09:00:00Z",
        "result": {
            "task_id": "fail0001",
            "status": "failed",
            "summary": "部署失败",
            "error": "HTTP 401 unauthorized",
            "score": 40,
            "finished_at": "2026-10-26T09:10:00Z",
            "acceptance": ["服务可访问", "日志已归档"],
            "checks": [
                {
                    "criterion": "服务可访问",
                    "passed": False,
                    "note": "连接被拒绝",
                },
                {"criterion": "日志已归档", "passed": True},
            ],
        },
    }
    goals = [
        {
            "task_id": "run00001",
            "goal": "正在执行的归档任务",
            "conversation_id": "c-run",
            "status": "running",
            "mode": "auto",
            "task_mode": "execute",
            "started_at": "2026-10-26T10:00:00Z",
        },
        failed,
        {
            "task_id": "done0001",
            "goal": "已完成的归档任务 1",
            "conversation_id": "c-ok",
            "status": "success",
            "mode": "auto",
            "task_mode": "execute",
            "started_at": "2026-10-26T08:30:00Z",
            "result": {
                "task_id": "done0001",
                "status": "success",
                "summary": "归档完成 1",
                "score": 100,
                "finished_at": "2026-10-26T08:40:00Z",
            },
        },
        {
            "task_id": "done0002",
            "goal": "已完成的归档任务 2",
            "conversation_id": "c-ok",
            "status": "success",
            "mode": "auto",
            "task_mode": "execute",
            "started_at": "2026-10-26T09:20:00Z",
            "result": {
                "task_id": "done0002",
                "status": "success",
                "summary": "归档完成 2",
                "score": 100,
                "finished_at": "2026-10-26T09:30:00Z",
            },
        },
        {
            "task_id": "cancel01",
            "goal": "已取消的清理任务",
            "conversation_id": "c-stop",
            "status": "cancelled",
            "mode": "auto",
            "task_mode": "execute",
            "started_at": "2026-10-26T04:00:00Z",
        },
        {
            "task_id": "wait0001",
            "goal": "等待批准的发布任务",
            "conversation_id": "c-wait",
            "status": "running",
            "mode": "auto",
            "task_mode": "execute",
            "started_at": "2026-10-26T09:40:00Z",
        },
        {
            "task_id": "part0001",
            "goal": "只完成一半的巡检任务",
            "conversation_id": "c-part",
            "status": "partial",
            "mode": "auto",
            "task_mode": "execute",
            "started_at": "2026-10-26T07:30:00Z",
            "result": {
                "task_id": "part0001",
                "status": "partial",
                "summary": "巡检完成 3/5 项",
                "score": 60,
                "finished_at": "2026-10-26T07:50:00Z",
            },
        },
    ]
    tags = {
        "c-run": ("处理中", "running"),
        "c-fail": ("失败", "failed"),
        "c-ok": ("成功", "success"),
        "c-stop": ("已停止", "cancelled"),
        "c-part": ("部分完成", "partial"),
    }
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        page = browser.new_page(viewport={"width": 1440, "height": 900})
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.route(
            "**/api/goals", lambda route: route.fulfill(json={"goals": goals})
        )
        page.route("**/api/goals/*", lambda route: route.fulfill(json=failed))
        page.route(
            "**/api/spaces",
            lambda route: route.fulfill(
                json={
                    "active_id": "default",
                    "spaces": [
                        {
                            "id": "default",
                            "name": "默认空间",
                            "is_default": True,
                        }
                    ],
                }
            ),
        )
        page.route(
            "**/api/conversations",
            lambda route: route.fulfill(json={"conversations": conversations}),
        )
        try:
            page.goto(base)
            page.wait_for_load_state("networkidle")
            # The start screen is the hero plus composer. Task cards stay
            # mounted as a state holder but never render above the input.
            expect(page.locator("#goals-empty")).to_be_visible()
            expect(page.locator("#goals-empty")).to_contain_text("把想法")
            expect(page.locator(".goal-card")).to_have_count(len(goals))
            expect(page.locator(".goal-card:visible")).to_have_count(0)
            for removed in (
                "#goal-filters",
                "#goals-more",
                "#goal-fail-banner",
                "#goals-filtered-empty",
            ):
                expect(page.locator(removed)).to_have_count(0)
            page.screenshot(
                path=str(output / "home-start.png"),
                animations="disabled",
            )

            page.locator("#sidebar-toggle").click()
            expect(page.locator("#sidebar")).to_be_visible()
            for convo_id, (label, tone) in tags.items():
                tag = page.locator(
                    f'.convo-item[data-convo-id="{convo_id}"] .convo-status'
                )
                expect(tag).to_have_text(label)
                expect(tag).to_have_attribute("data-status", tone)
            expect(
                page.locator(
                    '.convo-item[data-convo-id="c-idle"] .convo-status'
                )
            ).to_have_count(0)
            page.screenshot(
                path=str(output / "home-sidebar-tags.png"),
                animations="disabled",
            )

            # Finishing a task must not push a card back onto the start screen;
            # the sidebar tag is the record instead.
            page.evaluate("""() => {
                const t = tasks.get('run00001');
                const result = {
                    task_id: 'run00001',
                    status: 'success',
                    summary: '归档完成',
                    score: 100,
                    finished_at: '2026-10-26T10:05:00Z',
                };
                t.info.status = 'success';
                t.info.result = result;
                applyResult('run00001', result);
                refreshWorkbench();
            }""")
            expect(page.locator(".goal-card:visible")).to_have_count(0)
            expect(page.locator("#goals-empty")).to_be_visible()
            expect(
                page.locator('.convo-item[data-convo-id="c-run"] .convo-status')
            ).to_have_text("成功")

            # A pending approval is the loudest state and outranks the status.
            page.evaluate("""() => {
                const card = tasks.get('wait0001').card;
                const ap = document.createElement('div');
                ap.className = 'approval-card';
                card.appendChild(ap);
                paintConvoStatus();
            }""")
            waiting = page.locator(
                '.convo-item[data-convo-id="c-wait"] .convo-status'
            )
            expect(waiting).to_have_text("待批准")
            expect(waiting).to_have_attribute("data-status", "attention")
            page.evaluate("""() => {
                tasks.get('wait0001').card.querySelector('.approval-card').remove();
                paintConvoStatus();
            }""")
            expect(waiting).to_have_text("处理中")

            # Hover swaps the tag for rename/delete so the row keeps one
            # affordance.
            failed_item = page.locator('.convo-item[data-convo-id="c-fail"]')
            failed_item.hover()
            expect(failed_item.locator(".convo-status")).to_be_hidden()
            expect(failed_item.locator(".convo-item-ops")).to_be_visible()

            page.locator("#history-close").click()
            expect(page.locator("#sidebar")).not_to_be_visible()
            page.evaluate("() => openTaskDetail('fail0001')")
            overlay = page.locator("#modal-overlay")
            expect(overlay).to_be_visible()
            expect(page.locator("#modal")).to_contain_text("失败的部署任务")
            expect(
                page.locator("#modal").get_by_role(
                    "button", name="去配置 API Key"
                )
            ).to_be_visible()
            expect(page.locator("#modal")).to_contain_text("不会自动提交")
            page.locator("#modal").get_by_role(
                "button", name="重新填写"
            ).click()
            expect(overlay).to_be_hidden()
            expect(page.locator("#goal-input")).to_have_value("失败的部署任务")
            assert not errors, errors
        finally:
            page.close()
            browser.close()
        print(
            "PASS: start screen keeps only the hero; sidebar tags expose running, failure, completion, cancel, partial and approval state",
            flush=True,
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


def capture_stream_fixture(browser, base, mode="chat"):
    page = new_test_page(browser, base)
    try:
        conversation = create_test_conversation(page, "Stream regression")
        submitted = page.evaluate(
            """({id, mode}) => api('POST', '/api/goals', {
                goal: '回显流式回归检查：甲乙丙丁戊己庚辛壬癸。',
                task_mode: mode, mode: 'auto', conversation_id: id
            })""",
            {"id": conversation["id"], "mode": mode},
        )
        deadline = time.monotonic() + 30
        while True:
            detail = page.evaluate(
                "id => api('GET', '/api/goals/' + id)", submitted["task_id"]
            )
            if detail["status"] != "running" and detail.get("result"):
                break
            assert time.monotonic() < deadline, "mock task did not finish"
            page.evaluate("""async () => {
                const {approvals} = await api('GET', '/api/approvals');
                for (const ap of approvals || [])
                    await api('POST', '/api/approvals/' + ap.id, {approved: true});
            }""")
            time.sleep(0.05)
        conversation = page.evaluate(
            "id => api('GET', '/api/conversations/' + id)", conversation["id"]
        )
        return detail, conversation
    finally:
        page.close()


def running_snapshot(detail, events):
    snapshot = {
        **detail,
        "status": "running",
        "events": events,
        "progress_seq": max(
            (event["data"]["seq"] for event in events), default=0
        ),
        "chat_text": "".join(
            event["data"]["message"]
            for event in events
            if event["data"].get("phase") == "chat"
            and event["data"].get("kind") == "llm"
        ),
    }
    snapshot.pop("result", None)
    return snapshot


def stream_replay_page(browser, base, detail, conversation):
    deltas = [
        event
        for event in detail["events"]
        if event["type"] == "progress"
        and event["data"].get("phase") == "chat"
        and event["data"].get("kind") == "llm"
    ]
    assert len(deltas) >= 2, "mock capture must contain multiple real deltas"
    # Control timing and snapshots, never invent model or reasoning content.
    running = running_snapshot(detail, deltas[:1])
    page, state = replay_page(browser, base, detail, conversation, running)
    expect(page.locator(".msg--assistant .msg-content")).to_have_text(
        deltas[0]["data"]["message"]
    )
    return page, state, deltas


def replay_page(browser, base, detail, conversation, running):
    archived = {
        **conversation,
        "messages": [
            message
            for message in conversation["messages"]
            if message["role"] == "user"
        ],
    }
    state = {"goal": running, "conversation": archived}
    page = browser.new_page(viewport={"width": 1440, "height": 900})
    page.route(
        "**/api/goals",
        lambda route: route.fulfill(json={"goals": [state["goal"]]}),
    )
    page.route(
        f"**/api/goals/{detail['task_id']}",
        lambda route: route.fulfill(json=state["goal"]),
    )
    for suffix in ("", "/activate"):
        page.route(
            f"**/api/conversations/{conversation['id']}{suffix}",
            lambda route: route.fulfill(json=state["conversation"]),
        )
    page.goto(base)
    page.wait_for_load_state("networkidle")
    page.wait_for_function("() => sseConnection.readyState === 1")
    select_test_conversation(page, conversation)
    return page, state


def deliver_stream_event(page, event):
    # Replay captured transport data through the real registered SSE listeners.
    page.evaluate(
        """event => sseConnection.dispatchEvent(new MessageEvent(event.type, {
            data: JSON.stringify(event.data)
        }))""",
        event,
    )


def run_stream_checks(base, output):
    failures = []
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        try:
            detail, conversation = capture_stream_fixture(browser, base)
            (output / "stream-capture.json").write_text(
                json.dumps(detail, ensure_ascii=False, indent=2),
                encoding="utf-8",
            )
            for case in (
                "reconnect",
                "workbench-return",
                "snapshot-overlap",
                "stale-snapshot",
                "late-completion",
                "stale-running-snapshot",
            ):
                page, state, deltas = stream_replay_page(
                    browser, base, detail, conversation
                )
                errors = []
                page.on("pageerror", lambda error: errors.append(str(error)))
                try:
                    expected = "".join(d["data"]["message"] for d in deltas)
                    if case == "reconnect":
                        page.evaluate("() => sseConnection.close()")
                        state["goal"] = running_snapshot(detail, deltas)
                        with page.expect_response("**/api/goals") as snapshot:
                            page.evaluate("() => connectSSE()")
                        assert (
                            snapshot.value.json()["goals"][0]["events"]
                            == deltas
                        )
                        page.wait_for_load_state("networkidle")
                        expect(
                            page.locator(".msg--assistant .msg-content")
                        ).to_have_text(expected)
                    elif case == "workbench-return":
                        page.locator('.primary-nav [data-goto="goals"]').click()
                        page.wait_for_load_state("networkidle")
                        expect(page.locator("#view-goals")).not_to_have_class(
                            re.compile(r"\bthread-mode\b")
                        )
                        for event in deltas[1:]:
                            deliver_stream_event(page, event)
                        state["goal"] = running_snapshot(detail, deltas)
                        expect(page.locator(".goal-summary")).to_have_text(
                            expected
                        )
                        select_test_conversation(page, conversation)
                        expect(
                            page.locator(".msg--assistant .msg-content")
                        ).to_have_text(expected)
                    elif case in ("snapshot-overlap", "stale-snapshot"):
                        held = []
                        hold_snapshot = lambda route: held.append(route)
                        page.route("**/api/goals", hold_snapshot)
                        with page.expect_request("**/api/goals"):
                            page.evaluate(
                                "() => { window.recovery = resyncAfterReconnect(); }"
                            )
                        if case == "snapshot-overlap":
                            deliver_stream_event(page, deltas[-1])
                            page.wait_for_function(
                                "id => convoLive.get(id).progress.pending.size > 0 && progressJobs.size === 0",
                                arg=detail["task_id"],
                            )
                        else:
                            for event in deltas[1:]:
                                deliver_stream_event(page, event)
                        held[0].fulfill(
                            json={
                                "goals": [running_snapshot(detail, deltas[:-1])]
                            }
                        )
                        page.evaluate("() => window.recovery")
                        page.unroute("**/api/goals", hold_snapshot)
                        state["goal"] = running_snapshot(detail, deltas)
                        expect(
                            page.locator(".msg--assistant .msg-content")
                        ).to_have_text(expected)
                    else:
                        held = []
                        if case == "stale-running-snapshot":
                            hold_snapshot = lambda route: held.append(route)
                            page.route("**/api/goals", hold_snapshot)
                            with page.expect_request("**/api/goals"):
                                page.evaluate(
                                    "() => { window.recovery = resyncAfterReconnect(); }"
                                )
                        for event in deltas[1:-1]:
                            deliver_stream_event(page, event)
                        state.update(goal=detail, conversation=conversation)
                        deliver_stream_event(
                            page,
                            {"type": "completed", "data": detail["result"]},
                        )
                        expect(
                            page.locator(".msg--assistant .msg-content")
                        ).to_have_text(detail["result"]["summary"])
                        expect(
                            page.locator('#goal-feed [data-act="cancel"]')
                        ).to_have_count(0)
                        if held:
                            held[0].fulfill(
                                json={
                                    "goals": [running_snapshot(detail, deltas)]
                                }
                            )
                            page.evaluate("() => window.recovery")
                            page.unroute("**/api/goals", hold_snapshot)
                        deliver_stream_event(page, deltas[-1])
                        page.wait_for_load_state("networkidle")
                        expect(page.locator(".msg--assistant")).to_have_count(1)
                        expect(
                            page.locator('#goal-feed [data-act="cancel"]')
                        ).to_have_count(0)
                        expect(
                            page.locator(".msg--assistant .streaming")
                        ).to_have_count(0)
                    if state["goal"]["status"] == "running":
                        for _ in range(3):
                            page.evaluate("() => resyncAfterReconnect()")
                            page.evaluate(
                                "id => attachRunningTasksToConvo(id)",
                                conversation["id"],
                            )
                        for event in deltas:
                            deliver_stream_event(page, event)
                        expect(
                            page.locator(".msg--assistant .msg-content")
                        ).to_have_text(expected)
                        expect(page.locator(".msg--assistant")).to_have_count(1)
                        expect(
                            page.locator('#goal-feed [data-act="cancel"]')
                        ).to_have_count(1)
                    assert not errors, errors
                    print(f"PASS: stream {case}", flush=True)
                except AssertionError as error:
                    failures.append(case)
                    print(f"FAIL: stream {case}: {error}", flush=True)
                finally:
                    page.screenshot(path=str(output / f"stream-{case}.png"))
                    page.close()
            run_completion_history_overlap(browser, base, detail, conversation)
            run_work_stream_checks(browser, base, output)
            run_long_stream_checks(browser, base, output)
        finally:
            browser.close()
    assert not failures, f"stream regressions: {', '.join(failures)}"


def run_completion_history_overlap(browser, base, detail, conversation):
    page, state, _ = stream_replay_page(browser, base, detail, conversation)
    held = []
    stale = state["conversation"]

    def hold_history(route):
        if not held:
            held.append(route)
        else:
            route.fulfill(json=state["conversation"])

    pattern = f"**/api/conversations/{conversation['id']}"
    page.route(pattern, hold_history)
    try:
        with page.expect_request(pattern):
            page.evaluate("() => { window.recovery = resyncAfterReconnect(); }")
        state.update(goal=detail, conversation=conversation)
        deliver_stream_event(
            page, {"type": "completed", "data": detail["result"]}
        )
        expect(page.locator(".msg--assistant .msg-content")).to_have_text(
            detail["result"]["summary"]
        )
        held[0].fulfill(json=stale)
        page.evaluate("() => window.recovery")
        expect(page.locator(".msg--assistant .msg-content")).to_have_text(
            detail["result"]["summary"]
        )
        expect(page.locator(".msg--assistant")).to_have_count(1)
        expect(page.locator('#goal-feed [data-act="cancel"]')).to_have_count(0)
        print(
            "PASS: completion wins over in-flight stale conversation history",
            flush=True,
        )
    finally:
        page.close()


def run_work_stream_checks(browser, base, output):
    detail, conversation = capture_stream_fixture(browser, base, "work")
    (output / "work-stream-capture.json").write_text(
        json.dumps(detail, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    progress = [
        event for event in detail["events"] if event["type"] == "progress"
    ]
    delegate = [
        event for event in progress if event["data"]["phase"] == "delegate"
    ]
    approvals = [
        event for event in detail["events"] if event["type"] == "approval"
    ]
    assert len(delegate) >= 2 and approvals, (
        "capture must include real delegate stream and approval"
    )
    assert all(event["data"]["kind"] == "llm" for event in delegate)
    text = "".join(event["data"]["message"] for event in delegate)
    for status in ("success", "failed", "cancelled", "archived"):
        page, state = replay_page(
            browser,
            base,
            detail,
            conversation,
            running_snapshot(detail, progress[:1]),
        )
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        try:
            for event in progress[1:]:
                deliver_stream_event(page, event)
            output_row = page.locator(".msg-body .tl-run-item:has(.tl-kind)")
            expect(output_row).to_have_text("委派" + text)
            expect(page.locator(".msg--assistant .msg-content")).to_have_text(
                "执行中…"
            )
            expect(
                page.locator(".msg-body .tl-kind").filter(has_text="规划")
            ).to_have_count(1)
            run = page.locator(".msg-body .tl-run")
            expect(run).to_have_attribute("open", "")
            before = run.inner_text()
            state["goal"] = running_snapshot(detail, progress)
            for _ in range(3):
                page.evaluate("() => resyncAfterReconnect()")
            expect(output_row).to_have_text("委派" + text)
            assert run.inner_text() == before, (
                "snapshot duplicated execution/delegate progress"
            )
            if status == "success":
                page.screenshot(
                    path=str(output / "work-stream-live.png"),
                    animations="disabled",
                )
                page.locator('.primary-nav [data-goto="goals"]').click()
                select_test_conversation(page, conversation)
                expect(output_row).to_have_text("委派" + text)
            deliver_stream_event(page, approvals[0])
            expect(page.locator(".convo-approval")).to_be_visible()
            terminal = {
                **detail["result"],
                "status": "success" if status == "archived" else status,
            }
            state.update(
                goal={
                    **detail,
                    "status": terminal["status"],
                    "result": terminal,
                },
                conversation=conversation,
            )
            if status == "archived":
                page.evaluate("c => renderConvoMessages(c)", conversation)
            else:
                deliver_stream_event(
                    page, {"type": "completed", "data": terminal}
                )
                expect(run).not_to_have_attribute("open", "")
                expect(run).not_to_contain_text("进行中")
                expect(
                    page.locator(".msg-body .timeline-item--active")
                ).to_have_count(0)
            expect(
                page.locator('#goal-feed [data-act="cancel"]')
            ).to_have_count(0)
            expect(page.locator(".convo-approval")).to_have_count(0)
            expect(page.locator(".msg--assistant .msg-content")).to_have_text(
                detail["result"]["summary"]
            )
            page.evaluate("""() => {
                window.lateRailCalls = 0;
                window.contextRefreshes = 0;
                LiveRail.observe = () => window.lateRailCalls++;
                LiveRail.approval = () => window.lateRailCalls++;
                const original = loadContext;
                window.loadContext = () => { window.contextRefreshes++; return original(); };
            }""")
            for event in (delegate[-1], progress[0], approvals[0]):
                deliver_stream_event(page, event)
            # Only the event kind is synthetic; no model/reasoning text is invented.
            deliver_stream_event(
                page,
                {
                    "type": "progress",
                    "data": {
                        "task_id": detail["task_id"],
                        "phase": "context",
                        "message": "",
                    },
                },
            )
            assert page.evaluate("() => window.contextRefreshes") > 0
            assert page.evaluate("() => window.lateRailCalls") == 0
            # The server status must also protect a page with no terminal cache.
            page.evaluate("() => { tasks.clear(); finishedTasks.clear(); }")
            deliver_stream_event(page, approvals[0])
            page.wait_for_function(
                "id => ensureJobs.size === 0 && finishedTasks.has(id)",
                arg=detail["task_id"],
            )
            deliver_stream_event(page, delegate[-1])
            assert page.evaluate("() => window.lateRailCalls") == 0
            expect(page.locator(".msg--assistant")).to_have_count(1)
            expect(
                page.locator('#goal-feed [data-act="cancel"]')
            ).to_have_count(0)
            expect(
                page.locator(".msg--assistant .streaming, .convo-approval")
            ).to_have_count(0)
            assert not errors, errors
            print(
                f"PASS: work/delegate timeline and late {status} progress/approval",
                flush=True,
            )
        finally:
            page.screenshot(path=str(output / f"work-stream-{status}.png"))
            page.close()


def run_long_stream_checks(browser, base, output):
    detail, conversation = capture_stream_fixture(browser, base)
    (output / "long-stream-capture.json").write_text(
        json.dumps(detail, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    assert len(detail["events"]) == 300 and detail["progress_seq"] > 300
    assert detail["chat_text"] == detail["result"]["summary"]
    deltas = [
        event
        for event in detail["events"]
        if event["type"] == "progress" and event["data"]["kind"] == "llm"
    ]
    suffix = "".join(event["data"]["message"] for event in deltas[1:])
    initial = running_snapshot(detail, deltas[:1])
    initial["chat_text"] = detail["chat_text"][: -len(suffix)]
    assert len(initial["chat_text"]) > len(deltas[0]["data"]["message"])
    page, state = replay_page(browser, base, detail, conversation, initial)
    try:
        state["goal"] = {**detail, "status": "running"}
        state["goal"].pop("result")
        for _ in range(3):
            page.evaluate("() => resyncAfterReconnect()")
            deliver_stream_event(page, deltas[-1])
        assert (
            page.evaluate("id => convoLive.get(id).buf", detail["task_id"])
            == detail["chat_text"]
        )
        expect(page.locator(".msg--assistant .msg-content")).to_have_text(
            detail["chat_text"]
        )
        page.locator('.primary-nav [data-goto="goals"]').click()
        expect(page.locator(".goal-summary")).to_have_text(detail["chat_text"])
        select_test_conversation(page, conversation)
        expect(page.locator(".msg--assistant .msg-content")).to_have_text(
            detail["chat_text"]
        )
        print(
            "PASS: full chat text survives 300-event ring, repeated recovery and duplicate SSE",
            flush=True,
        )
    finally:
        page.screenshot(path=str(output / "long-stream.png"))
        page.close()


@contextmanager
def isolated_mock_server(binary, output, scripts=None):
    with tempfile.TemporaryDirectory(prefix="gleam-shell-") as directory:
        root = Path(directory)
        workspace = root / "workspace"
        workspace.mkdir()
        extra = []
        if scripts:
            script = root / "mock-script.json"
            script.write_text(
                json.dumps(scripts, ensure_ascii=False), encoding="utf-8"
            )
            extra = ["--mock-script", str(script)]
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        base = f"http://127.0.0.1:{port}"
        log_name = "stream-server.log" if scripts else "server.log"
        with (output / log_name).open("w", encoding="utf-8") as log:
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
                    *extra,
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
                yield base
            finally:
                server.terminate()
                server.wait(timeout=10)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--output", default="tmp/ui-shell-check")
    parser.add_argument(
        "--suite",
        choices=["all", "shell", "home", "terminal", "submit", "stream"],
        default="all",
    )
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    if args.suite != "stream":
        with isolated_mock_server(binary, output) as base:
            if args.suite in ("all", "shell"):
                run_checks(base, output)
            if args.suite in ("all", "home"):
                run_home_feed_checks(base, output)
            if args.suite in ("all", "terminal"):
                run_terminal_checks(base, output)
            if args.suite in ("all", "submit"):
                run_submit_checks(base, output)
    if args.suite in ("all", "stream"):
        # Plain public outputs, emitted by the real mock engine before replay.
        scripts = [
            {
                "kind": "chat_mode",
                "texts": [
                    "流式回归片段 0123456789 " * 12,
                    "完整长回复片段 0123456789 " * 900,
                ],
            },
            {"kind": "chat", "texts": ["子任务公开结果：样本校验通过。 " * 8]},
            {
                "kind": "plan",
                "texts": [
                    json.dumps(
                        {
                            "steps": [
                                {
                                    "id": "d1",
                                    "description": "独立校验",
                                    "tool": "agent.delegate",
                                    "args": {"goal": "返回公开的样本校验结果"},
                                },
                                {
                                    "id": "r1",
                                    "description": "回复",
                                    "tool": "reply",
                                    "args": {"text": "主任务已完成"},
                                    "depends_on": ["d1"],
                                },
                            ]
                        },
                        ensure_ascii=False,
                    )
                ],
            },
        ]
        with isolated_mock_server(binary, output, scripts) as base:
            run_stream_checks(base, output)


if __name__ == "__main__":
    main()
