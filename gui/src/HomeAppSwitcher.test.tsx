import { describe, expect, mock, test } from 'bun:test';
import { fireEvent, render, screen, within } from '@testing-library/react';
import {
  HomeAppSwitcher,
  isAllowedNavHref,
  normalizeHomeAppStatus,
  type HomeAppSwitcherItem,
  type HomeAppSwitcherLinkProps,
} from './HomeAppSwitcher';

const ORIGINS = ['https://apps.example.com'];

function tree(): HomeAppSwitcherItem[] {
  return [
    {
      key: 'mfmanager',
      label: 'MFManager',
      href: 'https://apps.example.com/manager',
      children: [
        { key: 'todo', label: 'MFTodo', href: 'https://apps.example.com/todo', status: 'running' },
        { key: 'notes', label: 'MFNotes', href: 'https://apps.example.com/notes', status: 'stopped' },
        { key: 'self', label: 'Self', href: 'https://apps.example.com/self', current: true, status: 'running' },
        { key: 'last', label: 'MFLast', href: 'https://apps.example.com/last', status: 'running' },
      ],
    },
  ];
}

const home = () => (
  <a data-testid="home" className="home-label" href="/tasks">
    MFTodo
  </a>
);

function renderSwitcher(props: Partial<React.ComponentProps<typeof HomeAppSwitcher>> = {}) {
  return render(<HomeAppSwitcher home={home()} items={tree()} allowedOrigins={ORIGINS} {...props} />);
}

const trigger = () => screen.getByRole('button');
const open = () => fireEvent.click(trigger());
const entries = () => screen.getAllByRole('menuitem');

describe('HomeAppSwitcher availability', () => {
  const homeOnly = render(home()).container.innerHTML;
  const cases: Array<[string, HomeAppSwitcherItem[] | null | undefined]> = [
    ['null', null],
    ['undefined', undefined],
    ['empty', []],
    ['root with no children', [{ key: 'r', label: 'R', href: 'https://apps.example.com/', children: [] }]],
    [
      'root with undefined children',
      [{ key: 'r', label: 'R', href: 'https://apps.example.com/', children: undefined as never }],
    ],
  ];
  for (const [name, items] of cases) {
    test(`unavailable (${name}) renders only home, no button or svg`, () => {
      const { container } = render(<HomeAppSwitcher home={home()} items={items} allowedOrigins={ORIGINS} />);
      expect(container.innerHTML).toBe(homeOnly);
      expect(container.querySelector('button')).toBeNull();
      expect(container.querySelector('svg')).toBeNull();
    });
  }

  test('home element is identical whether available or not', () => {
    const a = render(<HomeAppSwitcher home={home()} items={null} allowedOrigins={ORIGINS} />);
    const unavailable = a.container.querySelector('[data-testid="home"]')!;
    const unavailableHtml = unavailable.outerHTML;
    const unavailableClass = unavailable.className;
    a.unmount();
    const b = renderSwitcher();
    const available = b.container.querySelector('[data-testid="home"]')!;
    expect(available.outerHTML).toBe(unavailableHtml);
    expect(available.className).toBe(unavailableClass);
  });

  test('trigger a11y: haspopup, expanded toggling, default and custom name', () => {
    renderSwitcher();
    expect(trigger()).toHaveAttribute('aria-haspopup', 'menu');
    expect(trigger()).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByRole('button', { name: 'Switch application' })).toBe(trigger());
    open();
    expect(trigger()).toHaveAttribute('aria-expanded', 'true');
    expect(trigger().getAttribute('aria-controls')).toBe(screen.getByRole('menu').id);
    expect(trigger().querySelector('svg')).toHaveAttribute('aria-hidden', 'true');
    open();
    expect(trigger()).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('menu')).toBeNull();
  });

  test('custom switchLabel', () => {
    renderSwitcher({ switchLabel: 'Change app' });
    expect(screen.getByRole('button', { name: 'Change app' })).toBeTruthy();
  });
});

describe('HomeAppSwitcher menu structure', () => {
  test('lists roots then children in order with levels and one indent step', () => {
    renderSwitcher();
    open();
    const items = entries();
    expect(items.map((e) => e.textContent)).toEqual([
      'MFManager',
      'MFTodo',
      'MFNotesstopped',
      'Self',
      'MFLast',
    ]);
    expect(items[0]).toHaveAttribute('data-level', '0');
    expect(items[1]).toHaveAttribute('data-level', '1');
    expect(items[0].style.paddingInlineStart).toBe('calc(0.75rem + 0 * 1rem)');
    expect(items[1].style.paddingInlineStart).toBe('calc(0.75rem + 1 * 1rem)');
    const menu = screen.getByRole('menu');
    expect(menu.style.position).toBe('absolute');
    expect(menu.style.top).toBe('100%');
    expect(menu.style.zIndex).toBe('50');
  });

  test('navigable entries are anchors with href through the default renderer', () => {
    renderSwitcher();
    open();
    const todo = screen.getByText('MFTodo', { selector: 'span' }).closest('[role="menuitem"]')!;
    expect(todo.tagName).toBe('A');
    expect(todo).toHaveAttribute('href', 'https://apps.example.com/todo');
  });

  test('custom renderLink receives documented props and its output is used; ref forwarded', () => {
    const seen: HomeAppSwitcherLinkProps[] = [];
    renderSwitcher({
      renderLink: (p) => {
        seen.push(p);
        const { ref, ...rest } = p;
        return <a ref={ref} data-custom="yes" {...rest} />;
      },
    });
    open();
    expect(seen.length).toBeGreaterThan(0);
    const p = seen[0];
    expect(p.role).toBe('menuitem');
    expect(p.tabIndex).toBe(-1);
    expect(p['data-slot']).toBe('item');
    expect(p['data-level']).toBe(0);
    expect(typeof p.onClick).toBe('function');
    expect(typeof p.className).toBe('string');
    expect(document.querySelectorAll('[data-custom="yes"]').length).toBe(4);
    fireEvent.keyDown(trigger(), { key: 'ArrowDown' });
    expect(document.activeElement).toBe(document.querySelector('[data-custom="yes"]'));
  });
});

describe('HomeAppSwitcher origin policy', () => {
  const bad = ['https://evil.example.org/x', 'javascript:alert(1)', '/relative/path', 'http://[bad'];
  for (const href of bad) {
    test(`non-navigable href renders without anchor: ${href}`, () => {
      renderSwitcher({
        items: [
          {
            key: 'r',
            label: 'Root',
            href: 'https://apps.example.com/',
            children: [{ key: 'c', label: 'Kid', href, status: 'running' }],
          },
        ],
      });
      open();
      const kid = screen.getByText('Kid').closest('[role="menuitem"]')!;
      expect(kid.tagName).toBe('SPAN');
      expect(kid).not.toHaveAttribute('href');
      expect(kid).toHaveAttribute('aria-disabled', 'true');
    });
  }

  test('allowed origin with trailing slash/path still matches', () => {
    renderSwitcher({ allowedOrigins: ['https://apps.example.com/some/path/'] });
    open();
    expect(within(screen.getByRole('menu')).getByText('MFTodo').closest('a')).toBeTruthy();
  });

  test('empty allowedOrigins makes everything non-navigable', () => {
    renderSwitcher({ allowedOrigins: [] });
    open();
    expect(screen.getByRole('menu').querySelectorAll('a').length).toBe(0);
    for (const e of entries()) expect(e).toHaveAttribute('aria-disabled', 'true');
  });

  test('in-place mutation of allowedOrigins is honoured on re-render', () => {
    const origins = ['https://apps.example.com'];
    const { rerender } = renderSwitcher({ allowedOrigins: origins });
    open();
    expect(screen.getByRole('menu').querySelectorAll('a').length).toBeGreaterThan(0);
    origins.pop();
    rerender(<HomeAppSwitcher home={home()} items={tree()} allowedOrigins={origins} />);
    expect(screen.getByRole('menu').querySelectorAll('a').length).toBe(0);
  });

  test('isAllowedNavHref unit cases, invalid origins skipped', () => {
    expect(isAllowedNavHref('https://a.test/x', ['not a url', 'https://a.test'])).toBe(true);
    expect(isAllowedNavHref('https://a.test/x', ['not a url'])).toBe(false);
    expect(isAllowedNavHref('ftp://a.test/x', ['ftp://a.test'])).toBe(false);
    expect(isAllowedNavHref('http://a.test/x', ['https://a.test'])).toBe(false);
  });

  test('current child is not a link, has aria-current, skipped by arrows', () => {
    renderSwitcher();
    open();
    const self = screen.getByText('Self').closest('[role="menuitem"]')!;
    expect(self.tagName).toBe('SPAN');
    expect(self).toHaveAttribute('aria-current', 'true');
    expect(self).toHaveAttribute('aria-disabled', 'true');
    fireEvent.keyDown(trigger(), { key: 'ArrowDown' });
    const menu = screen.getByRole('menu');
    fireEvent.keyDown(menu, { key: 'ArrowDown' }); // todo
    fireEvent.keyDown(menu, { key: 'ArrowDown' }); // notes
    fireEvent.keyDown(menu, { key: 'ArrowDown' }); // skips Self -> last
    expect(document.activeElement).toHaveTextContent('MFLast');
  });
});

describe('HomeAppSwitcher text safety', () => {
  test('label and status markup render as literal text', () => {
    renderSwitcher({
      items: [
        {
          key: 'r',
          label: '<b>x</b>',
          href: 'https://apps.example.com/',
          children: [{ key: 'c', label: '<i>kid</i>', href: 'https://apps.example.com/c', status: '<b>failed</b>' }],
        },
      ],
    });
    open();
    expect(screen.getByRole('menu').querySelector('b')).toBeNull();
    expect(screen.getByRole('menu').querySelector('i')).toBeNull();
    expect(screen.getByText('<b>x</b>')).toBeTruthy();
    expect(screen.getByRole('menu').textContent).not.toContain('<b>failed</b>');
  });
});

describe('HomeAppSwitcher status', () => {
  function withStatus(status: unknown, extra: Partial<React.ComponentProps<typeof HomeAppSwitcher>> = {}) {
    const child = { key: 'c', label: 'Kid', href: 'https://apps.example.com/c' } as Record<string, unknown>;
    if (status !== '__omit__') child.status = status;
    const r = renderSwitcher({
      items: [
        {
          key: 'r',
          label: 'Root',
          href: 'https://apps.example.com/',
          children: [child as never],
        },
      ],
      ...extra,
    });
    open();
    return r;
  }
  const statusEl = () => document.querySelector('[data-slot="status"]');

  test('running renders no span and does not call formatStatus', () => {
    const formatStatus = mock(() => 'x');
    withStatus('running', { formatStatus });
    expect(statusEl()).toBeNull();
    expect(formatStatus).not.toHaveBeenCalled();
  });

  for (const s of ['pending', 'stopped', 'failed']) {
    test(`${s} renders its word with data-status`, () => {
      withStatus(s);
      expect(statusEl()).toHaveTextContent(s);
      expect(statusEl()).toHaveAttribute('data-status', s);
    });
  }

  for (const s of ['__omit__', undefined, '', '   ', 'weird', 'RUNNING']) {
    test(`unknown for ${JSON.stringify(s)}`, () => {
      withStatus(s);
      expect(statusEl()).toHaveTextContent('Status unknown');
      expect(statusEl()).toHaveAttribute('data-status', 'unknown');
      expect(screen.getByRole('menu').textContent).not.toContain('weird');
      expect(screen.getByRole('menu').textContent).not.toContain('RUNNING');
    });
  }

  test('unknown has the same classes as failed (not destructive)', () => {
    withStatus('failed');
    const failedClass = statusEl()!.className;
    cleanupAndRerender();
    function cleanupAndRerender() {
      document.body.innerHTML = '';
    }
    withStatus('weird');
    expect(statusEl()!.className).toBe(failedClass);
    expect(failedClass).not.toContain('destructive');
  });

  test('formatStatus receives normalized value and replaces text; null suppresses', () => {
    const formatStatus = mock((s: string) => `<<${s}>>`);
    withStatus('weird', { formatStatus });
    expect(formatStatus).toHaveBeenCalledWith('unknown');
    expect(statusEl()).toHaveTextContent('<<unknown>>');
  });

  test('formatStatus returning null suppresses the span', () => {
    withStatus('failed', { formatStatus: () => null });
    expect(statusEl()).toBeNull();
  });

  test('plain string status type-checks without a cast', () => {
    const raw: string = 'stopped';
    const items: HomeAppSwitcherItem[] = [
      { key: 'r', label: 'R', href: 'https://apps.example.com/', children: [{ key: 'c', label: 'C', href: 'https://apps.example.com/c', status: raw }] },
    ];
    renderSwitcher({ items });
    open();
    expect(statusEl()).toHaveAttribute('data-status', 'stopped');
  });

  test('normalizeHomeAppStatus unit cases', () => {
    expect(normalizeHomeAppStatus('pending')).toBe('pending');
    expect(normalizeHomeAppStatus('running')).toBe('running');
    expect(normalizeHomeAppStatus('stopped')).toBe('stopped');
    expect(normalizeHomeAppStatus('failed')).toBe('failed');
    for (const v of [undefined, null, '', '   ', 'weird', 'RUNNING', ' running']) {
      expect(normalizeHomeAppStatus(v)).toBe('unknown');
    }
  });
});

describe('HomeAppSwitcher dismissal and keyboard', () => {
  test('Escape closes, focuses trigger; onOpenChange true then false', () => {
    const onOpenChange = mock((_o: boolean) => {});
    renderSwitcher({ onOpenChange });
    expect(onOpenChange).not.toHaveBeenCalled();
    open();
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.activeElement).toBe(trigger());
    expect(onOpenChange.mock.calls.map((c) => c[0])).toEqual([true, false]);
  });

  for (const type of ['pointerdown', 'mousedown']) {
    test(`outside ${type} closes`, () => {
      renderSwitcher();
      open();
      fireEvent(document.body, new Event(type, { bubbles: true }));
      expect(screen.queryByRole('menu')).toBeNull();
    });
  }

  test('click inside menu does not close; clicking an entry closes', () => {
    renderSwitcher();
    open();
    fireEvent.mouseDown(screen.getByRole('menu'));
    fireEvent.pointerDown(screen.getByRole('menu'));
    expect(screen.queryByRole('menu')).not.toBeNull();
    fireEvent.click(within(screen.getByRole('menu')).getByText('MFTodo').closest('a')!);
    expect(screen.queryByRole('menu')).toBeNull();
  });

  test('click open does not auto-focus an entry', () => {
    renderSwitcher();
    open();
    expect(screen.getByRole('menu').contains(document.activeElement)).toBe(false);
  });

  test('ArrowDown on trigger opens with first navigable focused; ArrowUp last', () => {
    const a = renderSwitcher();
    fireEvent.keyDown(trigger(), { key: 'ArrowDown' });
    expect(document.activeElement).toHaveTextContent('MFManager');
    a.unmount();
    renderSwitcher();
    fireEvent.keyDown(trigger(), { key: 'ArrowUp' });
    expect(document.activeElement).toHaveTextContent('MFLast');
  });

  test('arrow movement wraps, Home/End jump, Tab closes', () => {
    renderSwitcher();
    fireEvent.keyDown(trigger(), { key: 'ArrowDown' });
    const menu = screen.getByRole('menu');
    fireEvent.keyDown(menu, { key: 'ArrowUp' });
    expect(document.activeElement).toHaveTextContent('MFLast');
    fireEvent.keyDown(menu, { key: 'ArrowDown' });
    expect(document.activeElement).toHaveTextContent('MFManager');
    fireEvent.keyDown(menu, { key: 'End' });
    expect(document.activeElement).toHaveTextContent('MFLast');
    fireEvent.keyDown(menu, { key: 'Home' });
    expect(document.activeElement).toHaveTextContent('MFManager');
    fireEvent.keyDown(menu, { key: 'Tab' });
    expect(screen.queryByRole('menu')).toBeNull();
  });
});

describe('HomeAppSwitcher classNames', () => {
  test('overrides reach each slot', () => {
    renderSwitcher({
      classNames: {
        root: 'c-root',
        trigger: 'c-trigger',
        icon: 'c-icon',
        menu: 'c-menu',
        item: 'c-item',
        rootItem: 'c-rootItem',
        childItem: 'c-childItem',
        currentItem: 'c-currentItem',
        disabledItem: 'c-disabledItem',
        status: 'c-status',
      },
    });
    open();
    expect(document.querySelector('[data-slot="home-app-switcher"]')).toHaveClass('c-root');
    expect(trigger()).toHaveClass('c-trigger');
    expect(trigger().querySelector('svg')).toHaveClass('c-icon');
    expect(screen.getByRole('menu')).toHaveClass('c-menu');
    const [root, todo, notes, self] = entries();
    expect(root).toHaveClass('c-item', 'c-rootItem');
    expect(todo).toHaveClass('c-item', 'c-childItem');
    expect(self).toHaveClass('c-currentItem', 'c-disabledItem');
    expect(notes.querySelector('[data-slot="status"]')).toHaveClass('c-status');
  });

  test('conflicting utility override replaces the default (tailwind-merge)', () => {
    renderSwitcher({ classNames: { menu: 'bg-red-500', trigger: 'text-blue-500' } });
    open();
    expect(screen.getByRole('menu')).toHaveClass('bg-red-500');
    expect(screen.getByRole('menu')).not.toHaveClass('bg-popover');
    expect(trigger()).toHaveClass('text-blue-500');
    expect(trigger()).not.toHaveClass('text-muted-foreground');
  });
});

describe('HomeAppSwitcher open state across unavailability', () => {
  test('closes when items become unavailable and stays closed when they return', () => {
    const onOpenChange = mock((_o: boolean) => {});
    const { rerender } = renderSwitcher({ onOpenChange });
    open();
    expect(onOpenChange).toHaveBeenCalledTimes(1);
    rerender(<HomeAppSwitcher home={home()} items={null} allowedOrigins={ORIGINS} onOpenChange={onOpenChange} />);
    expect(onOpenChange).toHaveBeenCalledTimes(2);
    expect(onOpenChange).toHaveBeenLastCalledWith(false);
    fireEvent.pointerDown(document.body);
    fireEvent.mouseDown(document.body);
    expect(onOpenChange).toHaveBeenCalledTimes(2);
    rerender(<HomeAppSwitcher home={home()} items={tree()} allowedOrigins={ORIGINS} onOpenChange={onOpenChange} />);
    expect(trigger()).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('menu')).toBeNull();
    expect(onOpenChange).toHaveBeenCalledTimes(2);
  });
});

describe('HomeAppSwitcher renderLink href fidelity warning', () => {
  const rewrite = (p: HomeAppSwitcherLinkProps) => <a {...p} href="/rewritten" />;

  test('warns when renderLink rewrites href', () => {
    const warn = mock((..._a: unknown[]) => {});
    const orig = console.warn;
    console.warn = warn as never;
    try {
      renderSwitcher({ renderLink: rewrite });
      open();
      expect(warn).toHaveBeenCalled();
    } finally {
      console.warn = orig;
    }
  });

  test('warns again when the same entry is later rewritten to a different href', () => {
    const warn = mock((..._a: unknown[]) => {});
    const orig = console.warn;
    console.warn = warn as never;
    try {
      const { rerender } = renderSwitcher({
        renderLink: (p) => <a {...p} href="/one" />,
      });
      open();
      const first = warn.mock.calls.length;
      expect(first).toBeGreaterThan(0);
      rerender(
        <HomeAppSwitcher
          home={home()}
          items={tree()}
          allowedOrigins={ORIGINS}
          renderLink={(p) => <a {...p} href="/two" />}
        />,
      );
      expect(warn.mock.calls.length).toBeGreaterThan(first);
    } finally {
      console.warn = orig;
    }
  });

  test('does not warn for the default renderer or a faithful renderLink', () => {
    const warn = mock((..._a: unknown[]) => {});
    const orig = console.warn;
    console.warn = warn as never;
    try {
      const a = renderSwitcher();
      open();
      a.unmount();
      renderSwitcher({ renderLink: (p) => <a {...p} /> });
      open();
      expect(warn).not.toHaveBeenCalled();
    } finally {
      console.warn = orig;
    }
  });
});

describe('HomeAppSwitcher dev detection without a process global', () => {
  test('missing process global does not throw; warning still works when process.env.NODE_ENV is available', () => {
    const warn = mock((..._a: unknown[]) => {});
    const orig = console.warn;
    const realProcess = globalThis.process;
    console.warn = warn as never;
    try {
      const a = renderSwitcher({ renderLink: (p) => <a {...p} href="/rewritten" /> });
      // `process` absent (browser bundle without replacement): ReferenceError is caught, no throw.
      (globalThis as { process?: unknown }).process = undefined;
      expect(() => open()).not.toThrow();
      expect(warn).not.toHaveBeenCalled();
      (globalThis as { process?: unknown }).process = realProcess;
      a.unmount();
      renderSwitcher({ renderLink: (p) => <a {...p} href="/rewritten" /> });
      open();
      expect(warn).toHaveBeenCalled();
    } finally {
      (globalThis as { process?: unknown }).process = realProcess;
      console.warn = orig;
    }
  });
});

describe('HomeAppSwitcher allowedOrigins robustness', () => {
  test('undefined or non-array allowedOrigins does not throw and nothing is navigable', () => {
    for (const bad of [undefined, null, {}, 'https://apps.example.com']) {
      const { unmount } = renderSwitcher({ allowedOrigins: bad as never });
      expect(() => open()).not.toThrow();
      expect(screen.queryAllByRole('menuitem').length).toBeGreaterThan(0);
      expect(screen.queryAllByRole('menuitem').some((e) => e.getAttribute('href'))).toBe(false);
      unmount();
    }
  });

  test('a normal array still allows its origin', () => {
    renderSwitcher();
    open();
    expect(entries().some((e) => e.getAttribute('href') === 'https://apps.example.com/todo')).toBe(true);
  });
});

describe('HomeAppSwitcher warn set bounds', () => {
  test('warns once per key+href+attr while open, and again after close and reopen', () => {
    const warn = mock((..._a: unknown[]) => {});
    const orig = console.warn;
    console.warn = warn as never;
    try {
      const { rerender } = renderSwitcher({ renderLink: (p) => <a {...p} href="/x" /> });
      open();
      const afterOpen = warn.mock.calls.length;
      expect(afterOpen).toBeGreaterThan(0);
      rerender(
        <HomeAppSwitcher
          home={home()}
          items={tree()}
          allowedOrigins={ORIGINS}
          renderLink={(p) => <a {...p} href="/x" />}
        />,
      );
      expect(warn.mock.calls.length).toBe(afterOpen);
      fireEvent.click(trigger()); // close
      open();
      expect(warn.mock.calls.length).toBe(afterOpen * 2);
    } finally {
      console.warn = orig;
    }
  });

  test('a renderer emitting a new href every render keeps warning without error', () => {
    const warn = mock((..._a: unknown[]) => {});
    const orig = console.warn;
    console.warn = warn as never;
    try {
      let n = 0;
      const mk = () => (p: HomeAppSwitcherLinkProps) => <a {...p} href={`/n${n++}`} />;
      const { rerender } = renderSwitcher({ renderLink: mk() });
      open();
      for (let i = 0; i < 100; i++) {
        rerender(<HomeAppSwitcher home={home()} items={tree()} allowedOrigins={ORIGINS} renderLink={mk()} />);
      }
      expect(warn.mock.calls.length).toBeGreaterThan(100);
    } finally {
      console.warn = orig;
    }
  });
});
