import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Modal } from "./Modal";
import { Icons } from "./icons";
import { searchText } from "../lib/table";

export function CommandPalette({ items, onClose }: { items: { to?: string; label: string; group?: string }[]; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [index, setIndex] = useState(0);
  const navigate = useNavigate();
  const matches = items.filter(item => item.to && searchText(`${item.label} ${item.group ?? ""}`).includes(searchText(query)));
  const active = Math.min(index, Math.max(0, matches.length - 1));
  function open(to?: string) { if (to) { navigate(to); onClose(); } }
  return <Modal title="Перейти к разделу" onClose={onClose} motion={false}>
    <div className="command-search"><Icons.search size={18} /><input data-autofocus className="input" aria-label="Поиск раздела" role="combobox" aria-autocomplete="list" aria-expanded="true" aria-controls="command-results" aria-activedescendant={matches.length ? `command-option-${active}` : undefined} placeholder="Заявки, подключения, брокеры…" value={query} onChange={e => { setQuery(e.target.value); setIndex(0); }} onKeyDown={e => {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); setIndex((active + (e.key === "ArrowDown" ? 1 : -1) + matches.length) % Math.max(1, matches.length)); }
      if (e.key === "Enter") { e.preventDefault(); open(matches[active]?.to); }
    }} /></div>
    <div id="command-results" role="listbox" className="command-results">{matches.map((item, i) => <div role="option" id={`command-option-${i}`} aria-selected={active === i} key={item.to}><button type="button" className={`command-result ${active === i ? "command-active" : ""}`} onClick={() => open(item.to)}><span>{item.label}<small>{item.group}</small></span><Icons.chevron size={16} /></button></div>)}{!matches.length && <p className="py-6 text-center text-sm text-muted">Раздел не найден</p>}</div>
    <div className="command-help"><span>↑ ↓ выбрать · Enter открыть</span><span>Esc закрыть</span></div>
  </Modal>;
}
