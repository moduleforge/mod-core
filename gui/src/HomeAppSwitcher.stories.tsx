import type { Story } from '@ladle/react';
import { HomeAppSwitcher, type HomeAppSwitcherItem } from './HomeAppSwitcher';

const ORIGINS = ['https://apps.example.com'];

const homeLink = (label: string) => (
  <a href="#" className="font-semibold">
    {label}
  </a>
);

const managerTree: HomeAppSwitcherItem[] = [
  {
    key: 'mfmanager',
    label: 'MFManager',
    href: 'https://apps.example.com/manager',
    children: [
      { key: 'todo', label: 'MFTodo', href: 'https://apps.example.com/todo', status: 'running' },
      { key: 'notes', label: 'MFNotes', href: 'https://apps.example.com/notes', status: 'stopped' },
      { key: 'self', label: 'MFManager (this app)', href: 'https://apps.example.com/manager', current: true },
    ],
  },
];

const todoTree: HomeAppSwitcherItem[] = [
  {
    key: 'mftodo',
    label: 'MFTodo',
    href: 'https://apps.example.com/todo',
    children: [{ key: 'manager', label: 'MFManager', href: 'https://apps.example.com/manager', status: 'running' }],
  },
];

export const Unavailable: Story = () => (
  <HomeAppSwitcher home={homeLink('MFTodo')} items={null} allowedOrigins={ORIGINS} />
);

export const MFManagerShaped: Story = () => (
  <div style={{ minHeight: 200 }}>
    <HomeAppSwitcher home={<span className="font-semibold">MFManager</span>} items={managerTree} allowedOrigins={ORIGINS} />
  </div>
);

export const MFTodoShaped: Story = () => (
  <div style={{ minHeight: 200 }}>
    <HomeAppSwitcher home={homeLink('MFTodo')} items={todoTree} allowedOrigins={ORIGINS} />
  </div>
);

export const ClassNamesOverride: Story = () => (
  <div style={{ minHeight: 200 }}>
    <HomeAppSwitcher
      home={homeLink('MFTodo')}
      items={managerTree}
      allowedOrigins={ORIGINS}
      classNames={{
        trigger: 'text-blue-600 hover:text-blue-800',
        menu: 'bg-white text-gray-900 border-gray-300',
        item: 'hover:bg-gray-100',
        status: 'text-gray-500 italic',
      }}
    />
  </div>
);
