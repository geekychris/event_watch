// CheatsheetCard — in-UI reference for every event type. Each row has
// "Inject →" which writes the topic/type/payload triple into the shared
// PublishFormContext, so the Publish card fills in and you can click
// Publish immediately.
//
// This is what makes the docs/cheatsheet.md truly live: not just a
// reference to read, but a set of one-click templates.
import { useState } from 'react';
import { EXAMPLES, GROUPS, type Example } from '../data/examples';
import { usePublishForm } from '../hooks/usePublishForm';

export function CheatsheetCard() {
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>({ pr: true, int: true });
  const [filter, setFilter] = useState('');
  const form = usePublishForm();

  const toggle = (k: string) => setOpenGroups((g) => ({ ...g, [k]: !g[k] }));

  const inject = (ex: Example) => {
    form.set({ topic: ex.topic, type: ex.type, payload: JSON.stringify(ex.payload) });
    // scroll the publish card into view so the user sees the change land.
    document.getElementById('publish-card')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };

  const norm = filter.toLowerCase().trim();
  const matches = (ex: Example) =>
    !norm || ex.label.toLowerCase().includes(norm)
          || ex.type.toLowerCase().includes(norm)
          || ex.topic.toLowerCase().includes(norm);

  return (
    <section className="card">
      <h2>Cheatsheet — event types &amp; payload examples</h2>
      <p className="hint">
        Every canned event is one click away. "Inject →" fills the Publish
        card above (Card 3); review then click Publish. Full reference in{' '}
        <code>docs/cheatsheet.md</code>.
      </p>
      <label>Filter
        <input value={filter} onChange={(e) => setFilter(e.target.value)}
               placeholder="pr, incr, deploy_started, chat/general, ..."
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>

      <div className="cheat-groups">
        {GROUPS.map((g) => {
          const items = EXAMPLES.filter((e) => e.group === g.key && matches(e));
          if (items.length === 0) return null;
          const isOpen = openGroups[g.key] ?? (norm.length > 0); // auto-open when filtering
          return (
            <div key={g.key} className="cheat-group">
              <button className="cheat-group-header" onClick={() => toggle(g.key)}>
                <span>{isOpen ? '▼' : '▶'} <b>{g.label}</b> <span className="hint">— {g.hint} · {items.length}</span></span>
              </button>
              {isOpen && (
                <table className="cheat-table">
                  <tbody>
                    {items.map((ex, i) => (
                      <tr key={i}>
                        <td className="cheat-label">{ex.label}</td>
                        <td className="cheat-payload">
                          <code>{ex.type}</code>
                          <br />
                          <code className="hint">{ex.topic}</code>
                          {ex.note && <div className="hint">↳ {ex.note}</div>}
                        </td>
                        <td className="cheat-json"><code>{JSON.stringify(ex.payload)}</code></td>
                        <td><button onClick={() => inject(ex)}>Inject →</button></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          );
        })}
      </div>
    </section>
  );
}
