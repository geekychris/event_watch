import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Publish card + scripted simulations (parity with the Wails app).
import { useState } from 'react';
import { useClient } from '../hooks/useClient';
const SIMS = {
    pr: { topic: 'pr/octo/hello/1', steps: [
            ['pr_opened', { title: 'Add feature', author: 'alice', base: 'main', head: 'abc123' }],
            ['pr_review_requested', { reviewer: 'bob' }],
            ['check_run_completed', { conclusion: 'success', name: 'test' }],
            ['pr_commented', {}],
            ['pr_reviewed', { state: 'approved' }],
            ['pr_merged', {}],
        ] },
    build: { topic: 'build/ci/42', steps: [
            ['build_queued', {}], ['build_started', {}],
            ['step_started', { step: 'compile' }], ['step_finished', { step: 'compile', status: 'success' }],
            ['step_started', { step: 'test' }], ['step_finished', { step: 'test', status: 'success' }],
            ['build_finished', { status: 'success' }],
        ] },
    deploy: { topic: 'deploy/prod/api', steps: [
            ['deploy_started', { version: 'v42', env: 'prod', service: 'api' }],
            ['health_check_pass', {}], ['deploy_finished', { status: 'success' }],
        ] },
    job: { topic: 'job/reindex-1', steps: [
            ['job_started', { name: 'reindex' }],
            ['job_progress', { percent: 33 }], ['job_log', { line: 'processing shard 1' }],
            ['job_progress', { percent: 66 }], ['job_log', { line: 'processing shard 2' }],
            ['job_progress', { percent: 100 }], ['job_finished', {}],
        ] },
    chat: { topic: 'chat/general', steps: [
            ['user_joined', { user: 'alice' }], ['user_joined', { user: 'bob' }],
            ['msg_posted', { id: 'm1', user: 'alice', text: 'hey team' }],
            ['msg_posted', { id: 'm2', user: 'bob', text: 'hi' }],
            ['msg_edited', { id: 'm1', text: 'hey team!' }],
        ] },
};
export function PublishCard() {
    const { client } = useClient();
    const [topic, setTopic] = useState('chat/general');
    const [type, setType] = useState('msg_posted');
    const [payload, setPayload] = useState('{"user":"alice","text":"hi"}');
    const publish = async () => {
        if (!client)
            return alert('connect first');
        let p;
        try {
            p = JSON.parse(payload || '{}');
        }
        catch (e) {
            return alert('payload must be JSON: ' + e.message);
        }
        try {
            await client.publish(topic, type, p);
        }
        catch (e) {
            alert('publish: ' + e.message);
        }
    };
    const runSim = async (kind) => {
        if (!client)
            return alert('connect first');
        const sim = SIMS[kind];
        for (const [t, p] of sim.steps) {
            try {
                await client.publish(sim.topic, t, p);
            }
            catch (e) {
                return alert('sim: ' + e.message);
            }
            await new Promise((r) => setTimeout(r, 250));
        }
    };
    return (_jsxs("section", { className: "card", children: [_jsx("h2", { children: "3. Publish / simulate" }), _jsxs("label", { children: ["Topic", _jsx("input", { value: topic, onChange: (e) => setTopic(e.target.value), autoCapitalize: "off", autoCorrect: "off", spellCheck: false })] }), _jsxs("label", { children: ["Event type", _jsx("input", { value: type, onChange: (e) => setType(e.target.value), autoCapitalize: "off", autoCorrect: "off", spellCheck: false })] }), _jsxs("label", { children: ["Payload JSON", _jsx("textarea", { rows: 3, value: payload, onChange: (e) => setPayload(e.target.value), autoCapitalize: "off", autoCorrect: "off", spellCheck: false })] }), _jsx("div", { className: "row", children: _jsx("button", { onClick: publish, children: "Publish" }) }), _jsx("h3", { children: "Scripted simulations" }), _jsx("p", { className: "hint", children: "Each button drives a full lifecycle on a default topic." }), _jsx("div", { className: "row", children: Object.keys(SIMS).map((k) => (_jsx("button", { onClick: () => runSim(k), children: k.toUpperCase() }, k))) })] }));
}
