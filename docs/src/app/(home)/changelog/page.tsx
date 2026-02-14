import { Badge } from 'lucide-react';

const changes = [
    {
        version: 'v1.0.1',
        date: '2025-01-15',
        title: 'Bug Fixes & Performance Improvements',
        description: 'Addressed issues with dependency injection in nested scopes and improved HTTP router performance.',
        type: 'patch',
    },
    {
        version: 'v1.0.0',
        date: '2025-01-01',
        title: 'Initial Release',
        description: 'Welcome to Forge! The first production-ready release including all core modules.',
        type: 'major',
    },
];

export default function ChangelogPage() {
    return (
        <main className="container max-w-4xl mx-auto px-6 py-16 md:py-24">
            <div className="mb-12">
                <h1 className="text-4xl font-bold mb-4">Changelog</h1>
                <p className="text-lg text-fd-muted-foreground">
                    Stay up to date with the latest changes and improvements to Forge.
                </p>
            </div>

            <div className="space-y-12">
                {changes.map((change, i) => (
                    <div key={i} className="relative pl-8 border-l border-fd-border">
                        <div className="absolute left-[-5px] top-0 size-2.5 rounded-full bg-fd-primary ring-4 ring-fd-background" />
                        <div className="flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-4 mb-2">
                            <span className="font-mono font-bold text-lg">{change.version}</span>
                            <span className="text-sm text-fd-muted-foreground">{change.date}</span>
                            <span className={`text-xs px-2 py-0.5 rounded-full border w-fit ${change.type === 'major' ? 'border-red-500 text-red-600 bg-red-500/10' : 'border-blue-500 text-blue-600 bg-blue-500/10'}`}>
                                {change.type}
                            </span>
                        </div>
                        <h3 className="text-xl font-semibold mb-2">{change.title}</h3>
                        <p className="text-fd-muted-foreground leading-relaxed">
                            {change.description}
                        </p>
                    </div>
                ))}
            </div>
        </main>
    );
}
