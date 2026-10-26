import type { Component } from 'svelte';

export interface MenuItem {
  type?: 'item';
  id: string;
  label: string;
  icon?: Component<{ size?: number | string }>;
  hint?: string;
  danger?: boolean;
  disabled?: boolean;
  onselect: () => void;
}

export interface MenuSeparator {
  type: 'separator';
}

export interface MenuHeading {
  type: 'heading';
  label: string;
}

export type MenuEntry = MenuItem | MenuSeparator | MenuHeading;
