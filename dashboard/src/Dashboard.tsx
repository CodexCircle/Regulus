import { Component, useState, type ErrorInfo, type ReactNode } from "react";
import ReactGridLayout, { useContainerWidth, type Layout } from "react-grid-layout";
import "react-grid-layout/css/styles.css";
import "react-resizable/css/styles.css";
import { addWidget, applyPositions, GRID_COLS, removeWidget, setWidgetConfig, type DashboardLayout, type WidgetInstance } from "./layout";
import { useCan, useSession } from "./session";
import { useDashboardLayout, type SaveState } from "./useDashboardLayout";
import { registry as defaultRegistry } from "./widgets";
import type { WidgetRegistry } from "./widgets/registry";

const ROW_HEIGHT = 56;

interface Props {
  registry?: WidgetRegistry;
  saveDelayMs?: number;
}

// Dashboard is the customizable grid of widgets. Each user arranges their own;
// the layout is saved on the hub.
export function Dashboard({ registry = defaultRegistry, saveDelayMs }: Props) {
  const session = useSession();
  const canEdit = useCan("layout.edit");
  const { loaded, saveState, update, reset } = useDashboardLayout(registry, {
    saveDelayMs,
    onUnauthorized: session?.expired,
  });
  const [editing, setEditing] = useState(false);
  const [picking, setPicking] = useState(false);
  const [confirmingReset, setConfirmingReset] = useState(false);

  if (loaded.status === "loading") return <p>Loading…</p>;
  if (loaded.status === "error") return <p role="alert">Could not load the dashboard: {loaded.message}</p>;
  const layout = loaded.layout;

  return (
    <section aria-label="Dashboard">
      <div className="toolbar">
        {canEdit && (
          <button type="button" aria-pressed={editing} onClick={() => {
            setEditing(!editing);
            setPicking(false);
            setConfirmingReset(false);
          }}>
            {editing ? "Done" : "Customize"}
          </button>
        )}
        {editing && canEdit && (
          <>
            <button type="button" aria-expanded={picking} onClick={() => setPicking(!picking)}>
              Add widget
            </button>
            {confirmingReset ? (
              <span>
                Replace your layout with the default?{" "}
                <button type="button" onClick={() => {
                  reset();
                  setConfirmingReset(false);
                }}>
                  Yes, reset
                </button>{" "}
                <button type="button" onClick={() => setConfirmingReset(false)}>
                  Cancel
                </button>
              </span>
            ) : (
              <button type="button" onClick={() => setConfirmingReset(true)}>
                Reset to default
              </button>
            )}
          </>
        )}
        <SaveStatus state={saveState} />
      </div>

      {editing && picking && (
        <WidgetPicker
          registry={registry}
          onPick={(id) => {
            const def = registry.get(id);
            if (def) update(addWidget(layout, def.id, def.defaultSize));
            setPicking(false);
          }}
        />
      )}

      {layout.widgets.length === 0 ? (
        <p>The dashboard is empty. Choose Customize, then Add widget.</p>
      ) : (
        <Grid layout={layout} registry={registry} editing={editing} onChange={update} />
      )}
    </section>
  );
}

function SaveStatus({ state }: { state: SaveState }) {
  const text = { idle: "", saving: "Saving…", saved: "Saved", error: "Could not save the layout" }[state];
  return (
    <span className="save-status" role="status" aria-live="polite">
      {text}
    </span>
  );
}

function WidgetPicker({ registry, onPick }: { registry: WidgetRegistry; onPick: (id: string) => void }) {
  return (
    <ul className="widget-picker" aria-label="Available widgets">
      {registry.list().map((def) => (
        <li key={def.id}>
          <div>
            <strong>{def.title}</strong>
            <p>{def.description}</p>
          </div>
          <button type="button" onClick={() => onPick(def.id)} aria-label={`Add ${def.title}`}>
            Add
          </button>
        </li>
      ))}
    </ul>
  );
}

interface GridProps {
  layout: DashboardLayout;
  registry: WidgetRegistry;
  editing: boolean;
  onChange: (layout: DashboardLayout) => void;
}

function Grid({ layout, registry, editing, onChange }: GridProps) {
  const { width, containerRef, mounted } = useContainerWidth();

  const items = layout.widgets.map((w) => {
    const min = registry.get(w.type)?.minSize;
    return { i: w.i, x: w.x, y: w.y, w: w.w, h: w.h, minW: min?.w, minH: min?.h };
  });

  // The grid also reports moves it makes on its own (compaction, a narrow
  // screen); only real changes become a new layout and a save.
  const onLayoutChange = (next: Layout) => {
    if (!editing) return;
    const updated = applyPositions(layout, next);
    if (updated !== layout) onChange(updated);
  };

  return (
    <div ref={containerRef} className={editing ? "grid editing" : "grid"}>
      {mounted && (
        <ReactGridLayout
          width={width}
          layout={items}
          gridConfig={{ cols: GRID_COLS, rowHeight: ROW_HEIGHT, margin: [12, 12] }}
          dragConfig={{ enabled: editing, handle: ".widget-title" }}
          resizeConfig={{ enabled: editing }}
          onLayoutChange={onLayoutChange}
        >
          {layout.widgets.map((w) => (
            <div key={w.i}>
              <WidgetFrame
                widget={w}
                registry={registry}
                editing={editing}
                onRemove={() => onChange(removeWidget(layout, w.i))}
                onConfig={(config) => onChange(setWidgetConfig(layout, w.i, config))}
              />
            </div>
          ))}
        </ReactGridLayout>
      )}
    </div>
  );
}

interface FrameProps {
  widget: WidgetInstance;
  registry: WidgetRegistry;
  editing: boolean;
  onRemove: () => void;
  onConfig: (config: Record<string, unknown>) => void;
}

// WidgetFrame draws the title bar and isolates failures: a widget that throws
// shows an error in its own frame and leaves the rest of the dashboard alone.
function WidgetFrame({ widget, registry, editing, onRemove, onConfig }: FrameProps) {
  const def = registry.get(widget.type);
  const title = def?.title ?? "Unavailable widget";
  return (
    <article className="widget" aria-label={title}>
      <header>
        <h3 className="widget-title">{title}</h3>
        {editing && (
          <button type="button" className="widget-remove" onClick={onRemove} aria-label={`Remove ${title}`}>
            ×
          </button>
        )}
      </header>
      <div className="widget-body">
        {def ? (
          <WidgetBoundary key={widget.i} title={title}>
            <def.component instanceId={widget.i} config={widget.config ?? {}} setConfig={onConfig} />
          </WidgetBoundary>
        ) : (
          <p>
            The widget type <code>{widget.type}</code> is not installed. You can remove it.
          </p>
        )}
      </div>
    </article>
  );
}

class WidgetBoundary extends Component<{ title: string; children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(`widget "${this.props.title}" failed`, error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return <p role="alert">This widget failed: {this.state.error.message}</p>;
    }
    return this.props.children;
  }
}
