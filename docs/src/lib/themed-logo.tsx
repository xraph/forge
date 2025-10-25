'use client'

import { ForgeLogo } from "@/components/logo";
import { useTheme } from "next-themes";
import { useMemo } from "react";


export function ThemedLogo() {
    const theme = useTheme()

    const isDark = useMemo(() => {
        return theme.resolvedTheme !== 'light'
    }, [theme])

    return <ForgeLogo size={20} color={isDark ? 'white': 'black'} animated />
}