import { Component, useState, type ErrorInfo, type ReactNode } from "react";
import ReactGridLayout, { useContainerWidth, type Layout } from "react-grid-layout";
import "react-grid-layout/css/styles.css";
import "react-resizable/css/styles.css";
import { addWidget, applyPositions, GRID_COLS, removeWidget, setWidgetConfig, type DashboardLayout, type WidgetInstance } from "./layout";
import { useT } from "./i18n";
import { useCan, useSession } from "./session";
import { useDashboardLayout, type SaveState } from "./useDashboardLayout";
import { registry as defaultRegistry } from "./widgets";
import { widgetDescription, widgetTitle, type WidgetRegistry } from "./widgets/registry";

const ROW_HEIGHT = 56;

interface Props {
  registry?: WidgetRegistry;
  saveDelayMs?: number;
}

// Dashboard is the customizable grid of widgets. Each user arranges their own;
// the layout is saved on the hub.
export function Dashboard({ registry = defaultRegistry, saveDelayMs }: Props) {
  const t = useT();
  const session = useSession();
  const canEdit = useCan("layout.edit");
  const { loaded, saveState, update, reset } = useDashboardLayout(registry, {
    saveDelayMs,
    onUnauthorized: session?.expired,
  });
  const [editing, setEditing] = useState(false);
  const [picking, setPicking] = useState(false);
  const [confirmingReset, setConfirmingReset] = useState(false);

  if (loaded.status === "loading") return <p>{t("servers.loading")}</p>;
  if (loaded.status === "error") return <p role="alert">{t("servers.dashboard.loadError", { message: loaded.message })}</p>;
  const layout = loaded.layout;

  return (
    <section aria-label={t("servers.dashboard.label")}>
      <div className="toolbar">
        {canEdit && (
          <button type="button" aria-pressed={editing} onClick={() => {
            setEditing(!editing);
            setPicking(false);
            setConfirmingReset(false);
          }}>
            {editing ? t("servers.dashboard.done") : t("servers.dashboard.customize")}
          </button>
        )}
        {editing && canEdit && (
          <>
            <button type="button" aria-expanded={picking} onClick={() => setPicking(!picking)}>
              {t("servers.dashboard.addWidget")}
            </button>
            {confirmingReset ? (
              <span>
                {t("servers.dashboard.confirmReset")}{" "}
                <button type="button" onClick={() => {
                  reset();
                  setConfirmingReset(false);
                }}>
                  {t("servers.dashboard.yesReset")}
                </button>{" "}
                <button type="button" onClick={() => setConfirmingReset(false)}>
                  {t("servers.dashboard.cancel")}
                </button>
              </span>
            ) : (
              <button type="button" onClick={() => setConfirmingReset(true)}>
                {t("servers.dashboard.reset")}
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
        <p>{t("servers.dashboard.empty")}</p>
      ) : (
        <Grid layout={layout} registry={registry} editing={editing} onChange={update} />
      )}
    </section>
  );
}

function SaveStatus({ state }: { state: SaveState }) {
  const t = useT();
  const text = {
    idle: "",
    saving: t("servers.dashboard.saving"),
    saved: t("servers.dashboard.saved"),
    error: t("servers.dashboard.saveError"),
  }[state];
  return (
    <span className="save-status" role="status" aria-live="polite">
      {text}
    </span>
  );
}

function WidgetPicker({ registry, onPick }: { registry: WidgetRegistry; onPick: (id: string) => void }) {
  const t = useT();
  return (
    <ul className="widget-picker" aria-label={t("servers.dashboard.available")}>
      {registry.list().map((def) => (
        <li key={def.id}>
          <div>
            <strong>{widgetTitle(def, t)}</strong>
            <p>{widgetDescription(def, t)}</p>
          </div>
          <button type="button" onClick={() => onPick(def.id)} aria-label={t("servers.dashboard.addNamed", { title: widgetTitle(def, t) })}>
            {t("servers.dashboard.add")}
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
  const t = useT();
  const def = registry.get(widget.type);
  const title = def ? widgetTitle(def, t) : t("servers.dashboard.unavailable");
  return (
    <article className="widget" aria-label={title}>
      <header>
        <h3 className="widget-title">{title}</h3>
        {editing && (
          <button type="button" className="widget-remove" onClick={onRemove} aria-label={t("servers.dashboard.removeNamed", { title })}>
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
            {t("servers.dashboard.notInstalledBefore")} <code>{widget.type}</code> {t("servers.dashboard.notInstalledAfter")}
          </p>
        )}
      </div>
    </article>
  );
}

function WidgetFailed({ message }: { message: string }) {
  const t = useT();
  return <p role="alert">{t("servers.dashboard.widgetFailed", { message })}</p>;
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
      return <WidgetFailed message={this.state.error.message} />;
    }
    return this.props.children;
  }
}
