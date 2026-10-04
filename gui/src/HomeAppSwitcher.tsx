import * as React from 'react';
import { ArrowLeftRight } from 'lucide-react';
import { cn } from './lib/utils';

export interface HomeAppSwitcherChild {
  key: string;
  /** Rendered strictly as text. */
  label: string;
  /** Absolute URL; navigable only if its origin is allowed. */
  href: string;
  /**
   * System-apps contract: `pending|running|stopped|failed`. A missing, empty or
   * unrecognized value is normalized to `'unknown'`.
   */
  status?: HomeAppSwitcherStatus | (string & {});
  /** `true` => non-navigating entry with `aria-current="true"`. */
  current?: boolean;
}

/** Normalized status the component works with and passes to `formatStatus`. */
export type HomeAppSwitcherStatus = 'pending' | 'running' | 'stopped' | 'failed' | 'unknown';

export interface HomeAppSwitcherItem {
  key: string;
  label: string;
  href: string;
  /** Never omitted by the contract; `undefined` is tolerated as `[]`. */
  children: HomeAppSwitcherChild[];
}

/**
 * Props handed to `renderLink`; the renderer MUST spread them onto an
 * anchor-like element (including `ref`, a regular prop in React 19) and render
 * `children` inside it.
 */
export interface HomeAppSwitcherLinkProps {
  href: string;
  children: React.ReactNode;
  className: string;
  role: 'menuitem';
  tabIndex: number;
  ref: React.Ref<HTMLAnchorElement>;
  onClick: React.MouseEventHandler<HTMLAnchorElement>;
  'data-slot': string;
  /** 0 = root, 1 = child. */
  'data-level': number;
}

export type HomeAppSwitcherSlot =
  | 'root'
  | 'trigger'
  | 'icon'
  | 'menu'
  | 'item'
  | 'rootItem'
  | 'childItem'
  | 'currentItem'
  | 'disabledItem'
  | 'status';

export interface HomeAppSwitcherProps {
  /** App-rendered home label/link, rendered verbatim and never altered. */
  home: React.ReactNode;
  /** `null`/`undefined` = not available. */
  items: readonly HomeAppSwitcherItem[] | null | undefined;
  /** Origins navigation is permitted to. */
  allowedOrigins: readonly string[];
  /** Default: `<a {...props} />`. */
  renderLink?: (props: HomeAppSwitcherLinkProps) => React.ReactNode;
  /** Receives the NORMALIZED status; never called for `running`. */
  formatStatus?: (status: HomeAppSwitcherStatus) => React.ReactNode;
  /** Trigger `aria-label`; default "Switch application". */
  switchLabel?: string;
  classNames?: Partial<Record<HomeAppSwitcherSlot, string>>;
  onOpenChange?: (open: boolean) => void;
}

const KNOWN_STATUSES: readonly HomeAppSwitcherStatus[] = ['pending', 'running', 'stopped', 'failed'];

/**
 * Normalize a raw status to the closed set the component works with. Anything
 * that is not exactly (case-sensitively) `pending|running|stopped|failed` —
 * including `undefined`, `null`, empty and whitespace-only — becomes
 * `'unknown'`. The raw string is never surfaced.
 */
export function normalizeHomeAppStatus(status: string | undefined | null): HomeAppSwitcherStatus {
  if (typeof status !== 'string') return 'unknown';
  return (KNOWN_STATUSES as readonly string[]).includes(status)
    ? (status as HomeAppSwitcherStatus)
    : 'unknown';
}

/**
 * Origin policy: `href` must parse as an absolute `http:`/`https:` URL and its
 * origin must equal the origin of one of `allowedOrigins` (each normalized
 * through `new URL(...).origin`; invalid entries are skipped, never thrown).
 */
export function isAllowedNavHref(href: string, allowedOrigins: readonly string[]): boolean {
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return false;
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return false;
  for (const o of allowedOrigins) {
    try {
      if (new URL(o).origin === url.origin) return true;
    } catch {
      // invalid allowed-origin entry: skip
    }
  }
  return false;
}

interface Entry {
  key: string;
  level: 0 | 1;
  label: string;
  href: string;
  status?: string;
  current: boolean;
  navigable: boolean;
}

const INDENT_BASE_REM = 0.75;
const INDENT_STEP_REM = 1;

function defaultStatusText(status: HomeAppSwitcherStatus): string {
  return status === 'unknown' ? 'Status unknown' : status;
}

/**
 * Router-agnostic home-app switcher. Renders the app-supplied `home` node
 * verbatim and, only when `items` has at least one child across all roots, a
 * separate trigger button that opens a menu of system applications. Layout is
 * inline-styled so the menu works with no CSS; cosmetics are token classes
 * overridable per slot through `classNames`.
 */
export function HomeAppSwitcher({
  home,
  items,
  allowedOrigins,
  renderLink,
  formatStatus,
  switchLabel,
  classNames,
  onOpenChange,
}: HomeAppSwitcherProps): React.ReactElement {
  const menuId = React.useId();
  const [open, setOpenState] = React.useState(false);
  const openRef = React.useRef(false);
  const onOpenChangeRef = React.useRef(onOpenChange);
  onOpenChangeRef.current = onOpenChange;
  const rootRef = React.useRef<HTMLDivElement>(null);
  const triggerRef = React.useRef<HTMLButtonElement>(null);
  const entryRefs = React.useRef<Array<HTMLElement | null>>([]);
  const pendingFocus = React.useRef<'first' | 'last' | null>(null);

  const setOpen = React.useCallback((next: boolean) => {
    if (openRef.current === next) return;
    openRef.current = next;
    setOpenState(next);
    onOpenChangeRef.current?.(next);
  }, []);

  const entries = React.useMemo<Entry[]>(() => {
    const out: Entry[] = [];
    for (const root of items ?? []) {
      out.push({
        key: `root:${root.key}`,
        level: 0,
        label: root.label,
        href: root.href,
        current: false,
        navigable: isAllowedNavHref(root.href, allowedOrigins),
      });
      for (const child of root.children ?? []) {
        const current = child.current === true;
        out.push({
          key: `child:${root.key}:${child.key}`,
          level: 1,
          label: child.label,
          href: child.href,
          status: child.status,
          current,
          navigable: !current && isAllowedNavHref(child.href, allowedOrigins),
        });
      }
    }
    return out;
  }, [items, allowedOrigins]);

  const childCount = entries.reduce((n, e) => n + (e.level === 1 ? 1 : 0), 0);
  const available = childCount > 0;

  const navigableIndexes = (): number[] =>
    entries.flatMap((e, i) => (e.navigable ? [i] : []));

  const focusIndex = (i: number | undefined) => {
    if (i === undefined) return;
    entryRefs.current[i]?.focus();
  };

  // Keyboard-open focus: runs after the menu has rendered.
  React.useEffect(() => {
    if (!open || !pendingFocus.current) return;
    const nav = entries.flatMap((e, i) => (e.navigable ? [i] : []));
    focusIndex(pendingFocus.current === 'first' ? nav[0] : nav[nav.length - 1]);
    pendingFocus.current = null;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Outside dismissal, registered only while open.
  React.useEffect(() => {
    if (!open) return;
    const onOutside = (event: Event) => {
      const root = rootRef.current;
      if (root && event.target instanceof Node && root.contains(event.target)) return;
      setOpen(false);
    };
    document.addEventListener('pointerdown', onOutside);
    document.addEventListener('mousedown', onOutside);
    return () => {
      document.removeEventListener('pointerdown', onOutside);
      document.removeEventListener('mousedown', onOutside);
    };
  }, [open, setOpen]);

  if (!available) {
    return <>{home}</>;
  }

  const openWithFocus = (which: 'first' | 'last') => {
    if (openRef.current) {
      const nav = navigableIndexes();
      focusIndex(which === 'first' ? nav[0] : nav[nav.length - 1]);
      return;
    }
    pendingFocus.current = which;
    setOpen(true);
  };

  const onTriggerKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      openWithFocus('first');
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      openWithFocus('last');
    } else if (event.key === 'Escape' && openRef.current) {
      setOpen(false);
    }
  };

  const onMenuKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const nav = navigableIndexes();
    if (event.key === 'Tab') {
      setOpen(false);
      return;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      setOpen(false);
      triggerRef.current?.focus();
      return;
    }
    if (nav.length === 0) return;
    const active = document.activeElement;
    const pos = nav.findIndex((i) => entryRefs.current[i] === active);
    let target: number | undefined;
    switch (event.key) {
      case 'ArrowDown':
        target = nav[pos < 0 ? 0 : (pos + 1) % nav.length];
        break;
      case 'ArrowUp':
        target = nav[pos < 0 ? nav.length - 1 : (pos - 1 + nav.length) % nav.length];
        break;
      case 'Home':
        target = nav[0];
        break;
      case 'End':
        target = nav[nav.length - 1];
        break;
      default:
        return;
    }
    event.preventDefault();
    focusIndex(target);
  };

  const onEntryClick = () => setOpen(false);

  const renderEntry = (entry: Entry, index: number) => {
    const baseClass = cn(
      'flex items-center gap-2 px-3 py-1.5 whitespace-nowrap outline-none',
      'hover:bg-muted focus-visible:bg-muted',
      classNames?.item,
      entry.level === 0 ? classNames?.rootItem : classNames?.childItem,
      entry.current && classNames?.currentItem,
      !entry.navigable && classNames?.disabledItem,
    );
    const style: React.CSSProperties = {
      paddingInlineStart: `calc(${INDENT_BASE_REM}rem + ${entry.level} * ${INDENT_STEP_REM}rem)`,
    };

    let statusNode: React.ReactNode = null;
    if (entry.level === 1) {
      const normalized = normalizeHomeAppStatus(entry.status);
      if (normalized !== 'running') {
        const text = formatStatus ? formatStatus(normalized) : defaultStatusText(normalized);
        if (text !== null && text !== false && text !== undefined) {
          statusNode = (
            <span
              data-slot="status"
              data-status={normalized}
              className={cn('ms-auto ps-3 text-xs text-muted-foreground', classNames?.status)}
            >
              {text}
            </span>
          );
        }
      }
    }
    const content = (
      <>
        <span>{entry.label}</span>
        {statusNode}
      </>
    );

    if (entry.navigable) {
      const linkProps: HomeAppSwitcherLinkProps = {
        href: entry.href,
        children: content,
        className: baseClass,
        role: 'menuitem',
        tabIndex: -1,
        ref: (el: HTMLAnchorElement | null) => {
          entryRefs.current[index] = el;
        },
        onClick: onEntryClick,
        'data-slot': 'item',
        'data-level': entry.level,
      };
      const rendered = renderLink ? renderLink(linkProps) : <a {...linkProps} />;
      return (
        <React.Fragment key={entry.key}>
          {React.isValidElement(rendered)
            ? React.cloneElement(rendered as React.ReactElement<{ style?: React.CSSProperties }>, {
                style: {
                  ...((rendered.props as { style?: React.CSSProperties }).style ?? {}),
                  ...style,
                },
              })
            : rendered}
        </React.Fragment>
      );
    }

    return (
      <span
        key={entry.key}
        role="menuitem"
        aria-disabled="true"
        aria-current={entry.current ? 'true' : undefined}
        data-slot="item"
        data-level={entry.level}
        className={baseClass}
        style={style}
        ref={(el) => {
          entryRefs.current[index] = el;
        }}
      >
        {content}
      </span>
    );
  };

  entryRefs.current.length = entries.length;

  return (
    <div
      ref={rootRef}
      data-slot="home-app-switcher"
      data-mf-component="home-app-switcher"
      className={cn('inline-flex items-center gap-1', classNames?.root)}
      style={{ position: 'relative' }}
    >
      {home}
      <button
        ref={triggerRef}
        type="button"
        data-slot="trigger"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        aria-label={switchLabel ?? 'Switch application'}
        className={cn(
          'inline-flex items-center justify-center rounded-sm text-muted-foreground hover:text-foreground',
          'outline-none focus-visible:ring-2 focus-visible:ring-ring',
          classNames?.trigger,
        )}
        onClick={() => setOpen(!openRef.current)}
        onKeyDown={onTriggerKeyDown}
      >
        <ArrowLeftRight aria-hidden="true" className={cn('size-4', classNames?.icon)} />
      </button>
      {open && (
        <div
          id={menuId}
          role="menu"
          data-slot="menu"
          className={cn(
            'bg-popover text-popover-foreground border border-border rounded-md shadow-md py-1 text-sm',
            classNames?.menu,
          )}
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            zIndex: 50,
            minWidth: 'max-content',
          }}
          onKeyDown={onMenuKeyDown}
        >
          {entries.map(renderEntry)}
        </div>
      )}
    </div>
  );
}
