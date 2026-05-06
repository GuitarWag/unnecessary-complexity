export interface CopyButtonProps {
    /** Text to copy to clipboard. */
    text: string;
    /** Idle button label. */
    label?: string;
    /** Label shown after a successful copy. */
    copiedLabel?: string;
    /** How long to show copiedLabel before reverting, in ms. */
    resetMs?: number;
}
export declare function CopyButton({ text, label, copiedLabel, resetMs, }: CopyButtonProps): import("react/jsx-runtime").JSX.Element;
