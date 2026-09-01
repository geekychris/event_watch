// Single-page controller for event_watch. One WebSocket, request/response via
// req_id, event delivery via a listener list. No framework — the surface is
// small and I want it obvious what's happening.
(() => {
  const $ = (id) => document.getElementById(id);
  const feed = $("event-feed");
  const status = $("conn-status");
  const btnConnect = $("btn-connect");
  const btnDisconnect = $("btn-disconnect");

  let ws = null;
  let baseHTTP = null;
  let authToken = "";
  let nextReq = 0;
  const pending = new Map(); // req_id → resolve

  function setStatus(kind, text) {
    status.className = "pill " + kind;
    status.textContent = text;
  }

  function objectTypeOf(topic) {
    const i = topic.indexOf("/");
    return i > 0 ? topic.slice(0, i) : "";
  }

  function appendEvent(e) {
    const div = document.createElement("div");
    div.className = "ev";
    const badge = `<span class="badge ${objectTypeOf(e.topic)}">${e.type}</span>`;
    div.innerHTML = `${badge}<code>${e.topic}</code> seq=${e.seq} ` +
                    (e.payload ? `<code>${escapeHtml(JSON.stringify(e.payload))}</code>` : "");
    feed.appendChild(div);
    feed.scrollTop = feed.scrollHeight;
  }

  function escapeHtml(s) {
    return s.replace(/[&<>"']/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"})[c]);
  }

  function send(op, extra = {}) {
    if (!ws || ws.readyState !== WebSocket.OPEN) throw new Error("not connected");
    ws.send(JSON.stringify({ op, ...extra }));
  }

  function request(op, extra = {}) {
    return new Promise((resolve, reject) => {
      const req_id = "r" + (++nextReq);
      pending.set(req_id, { resolve, reject });
      try { send(op, { ...extra, req_id }); }
      catch (err) { pending.delete(req_id); reject(err); }
    });
  }

  function handleFrame(f) {
    if (f.type === "event") {
      appendEvent(f.event);
      return;
    }
    if (f.type === "lagging") {
      appendEvent({ topic: f.topic, type: "lagging", seq: 0, payload: { missed: f.missed } });
      return;
    }
    if (f.req_id && pending.has(f.req_id)) {
      const { resolve, reject } = pending.get(f.req_id);
      pending.delete(f.req_id);
      if (f.type === "error") reject(new Error(f.message));
      else resolve(f);
    }
  }

  // -- connect --

  btnConnect.onclick = () => {
    const url = $("ws-url").value.trim();
    authToken = $("ws-token").value.trim();
    baseHTTP = url.replace(/^ws/, "http").replace(/\/ws$/, "");
    let full = url;
    if (authToken) full += (url.includes("?") ? "&" : "?") + "access_token=" + encodeURIComponent(authToken);
    setStatus("offline", "connecting…");
    ws = new WebSocket(full);
    ws.onopen = () => {
      setStatus("online", "connected");
      btnConnect.disabled = true; btnDisconnect.disabled = false;
      refreshTopics();
    };
    ws.onclose = () => {
      setStatus("offline", "disconnected");
      btnConnect.disabled = false; btnDisconnect.disabled = true;
    };
    ws.onerror = () => setStatus("offline", "error");
    ws.onmessage = (m) => { try { handleFrame(JSON.parse(m.data)); } catch (_) {} };
  };
  btnDisconnect.onclick = () => ws && ws.close();

  // -- subscribe --

  $("btn-subscribe").onclick = () => {
    const topic = $("sub-topic").value.trim();
    const from = $("sub-from").value;
    if (!topic) return;
    try { send("subscribe", { topic, from }); } catch (e) { alert(e.message); }
  };
  $("btn-unsubscribe").onclick = () => {
    const topic = $("sub-topic").value.trim();
    if (!topic) return;
    try { send("unsubscribe", { topic }); } catch (e) { alert(e.message); }
  };

  // -- publish --

  $("btn-publish").onclick = async () => {
    const topic = $("pub-topic").value.trim();
    const type = $("pub-type").value.trim();
    let payload;
    try { payload = JSON.parse($("pub-payload").value || "{}"); }
    catch (e) { return alert("payload must be valid JSON: " + e.message); }
    if (!topic || !type) return alert("topic and type required");
    try { await request("publish", { topic, type, payload }); } catch (e) { alert(e.message); }
  };

  // -- simulations --

  const SIMS = {
    pr: { topic: "pr/octo/hello/1", steps: [
      { type: "pr_opened", payload: { title: "Add feature", author: "alice", base: "main", head: "abc123" } },
      { type: "pr_review_requested", payload: { reviewer: "bob" } },
      { type: "check_run_completed", payload: { conclusion: "success", name: "test" } },
      { type: "pr_commented", payload: {} },
      { type: "pr_reviewed", payload: { state: "approved" } },
      { type: "pr_merged", payload: {} },
    ]},
    build: { topic: "build/ci/42", steps: [
      { type: "build_queued", payload: {} },
      { type: "build_started", payload: {} },
      { type: "step_started", payload: { step: "compile" } },
      { type: "step_finished", payload: { step: "compile", status: "success" } },
      { type: "step_started", payload: { step: "test" } },
      { type: "step_finished", payload: { step: "test", status: "success" } },
      { type: "build_finished", payload: { status: "success" } },
    ]},
    deploy: { topic: "deploy/prod/api", steps: [
      { type: "deploy_started", payload: { version: "v42", env: "prod", service: "api" } },
      { type: "health_check_pass", payload: {} },
      { type: "deploy_finished", payload: { status: "success" } },
    ]},
    job: { topic: "job/reindex-1", steps: [
      { type: "job_started", payload: { name: "reindex" } },
      { type: "job_progress", payload: { percent: 33 } },
      { type: "job_log", payload: { line: "processing shard 1" } },
      { type: "job_progress", payload: { percent: 66 } },
      { type: "job_log", payload: { line: "processing shard 2" } },
      { type: "job_progress", payload: { percent: 100 } },
      { type: "job_finished", payload: {} },
    ]},
    chat: { topic: "chat/general", steps: [
      { type: "user_joined", payload: { user: "alice" } },
      { type: "user_joined", payload: { user: "bob" } },
      { type: "msg_posted", payload: { id: "m1", user: "alice", text: "hey team" } },
      { type: "msg_posted", payload: { id: "m2", user: "bob", text: "hi" } },
      { type: "msg_edited", payload: { id: "m1", text: "hey team!" } },
    ]},
  };

  document.querySelectorAll("[data-sim]").forEach((btn) => {
    btn.onclick = async () => {
      const kind = btn.dataset.sim;
      const sim = SIMS[kind];
      if (!sim) return;
      for (const step of sim.steps) {
        try { await request("publish", { topic: sim.topic, ...step }); }
        catch (e) { alert(e.message); return; }
        await new Promise((r) => setTimeout(r, 250));
      }
    };
  });

  // -- state --

  $("btn-get-state").onclick = async () => {
    const topic = $("state-topic").value.trim();
    if (!topic) return;
    try {
      const f = await request("get_state", { topic });
      $("state-out").textContent = f.state ? JSON.stringify(f.state, null, 2) : "(no state yet)";
    } catch (e) { $("state-out").textContent = "error: " + e.message; }
  };

  // -- metrics + topics polling --

  async function refreshMetrics() {
    if (!baseHTTP) return;
    try {
      const r = await fetch(baseHTTP + "/admin/metrics.json", authToken ? { headers: { Authorization: "Bearer " + authToken } } : undefined);
      if (!r.ok) return;
      const m = await r.json();
      const rows = [
        ["connected clients", m.connected_clients],
        ["topics", m.topics],
      ];
      for (const [k, v] of Object.entries(m.subscriptions_by_type || {})) rows.push(["subs · " + k, v]);
      for (const [k, v] of Object.entries(m.ingested_by_type || {}))     rows.push(["ingested · " + k, v]);
      for (const [k, v] of Object.entries(m.fanned_out_by_type || {}))   rows.push(["fanned out · " + k, v]);
      for (const [k, v] of Object.entries(m.dropped_by_type || {}))      rows.push(["dropped · " + k, v]);
      $("metrics-table").innerHTML = "<tbody>" +
        rows.map(([k, v]) => `<tr><td>${k}</td><td>${v}</td></tr>`).join("") + "</tbody>";
    } catch (_) {}
  }

  async function refreshTopics() {
    if (!baseHTTP) return;
    try {
      const r = await fetch(baseHTTP + "/topics", authToken ? { headers: { Authorization: "Bearer " + authToken } } : undefined);
      if (!r.ok) return;
      const { topics } = await r.json();
      $("topics-list").innerHTML = (topics || []).map((t) => `<li><code>${t}</code></li>`).join("");
    } catch (_) {}
  }

  setInterval(refreshMetrics, 2000);
  setInterval(refreshTopics, 5000);
})();
