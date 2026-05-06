import type { ReactNode } from 'react';
export interface CreateButtonProps {
    onClick?: () => void | Promise<void>;
    disabled?: boolean;
    /** Shown while an async onClick is pending. */
    busyLabel?: string;
    children?: ReactNode;
    variant?: 'primary' | 'secondary';
}
export declare function CreateButton({ onClick, disabled, busyLabel, children, variant, }: CreateButtonProps): import("react/jsx-runtime").JSX.Element;
