# Forge Framework - Licensing Guide

This document explains the licensing structure for the Forge framework and its extensions.

## 📜 Overview

Forge uses a **dual-licensing approach**:

1. **Forge Core Framework**: MIT License (permissive, open source)
2. **AI Extension**: Commercial Source-Available License (restrictive)
3. **All Other Extensions**: MIT License (permissive, open source)

## 🎯 Quick Reference

| Component | License | Commercial Use | Redistribution |
|-----------|---------|----------------|----------------|
| Forge Core | MIT | ✅ Free | ✅ Yes |
| Auth Extension | MIT | ✅ Free | ✅ Yes |
| Cache Extension | MIT | ✅ Free | ✅ Yes |
| Consensus Extension | MIT | ✅ Free | ✅ Yes |
| Dashboard Extension | MIT | ✅ Free | ✅ Yes |
| Database Extension | MIT | ✅ Free | ✅ Yes |
| Events Extension | MIT | ✅ Free | ✅ Yes |
| GraphQL Extension | MIT | ✅ Free | ✅ Yes |
| gRPC Extension | MIT | ✅ Free | ✅ Yes |
| HLS Extension | MIT | ✅ Free | ✅ Yes |
| Kafka Extension | MIT | ✅ Free | ✅ Yes |
| MCP Extension | MIT | ✅ Free | ✅ Yes |
| MQTT Extension | MIT | ✅ Free | ✅ Yes |
| oRPC Extension | MIT | ✅ Free | ✅ Yes |
| Queue Extension | MIT | ✅ Free | ✅ Yes |
| Search Extension | MIT | ✅ Free | ✅ Yes |
| Storage Extension | MIT | ✅ Free | ✅ Yes |
| Streaming Extension | MIT | ✅ Free | ✅ Yes |
| WebRTC Extension | MIT | ✅ Free | ✅ Yes |
| **AI Extension** | **Commercial** | **❌ License Required** | **❌ No** |

## 📖 License Details

### Forge Core & Most Extensions (MIT License)

**Location**: `LICENSE` (root directory)

The MIT License is one of the most permissive open source licenses. You can:

- ✅ Use for commercial purposes
- ✅ Modify the code
- ✅ Distribute copies
- ✅ Sublicense
- ✅ Use in proprietary software

**Requirements**:
- Include the original copyright notice
- Include the license text

**No Warranty**: The software is provided "as is" without warranty.

### AI Extension (Commercial Source-Available License)

**Location**: `extensions/ai/LICENSE`

The AI Extension uses a more restrictive license because it contains proprietary algorithms and represents significant R&D investment.

#### Free Use Cases

You CAN use the AI Extension for FREE for:
- Personal projects
- Educational purposes
- Research and academic use
- Internal evaluation (90 days)
- Learning and studying the code

#### Commercial License Required

You NEED a paid commercial license for:
- Production deployments in commercial environments
- SaaS products or services
- Internal tools that generate revenue or cost savings
- Any commercial advantage use

#### Prohibited Without Permission

You CANNOT:
- Redistribute the AI Extension
- Build competing AI products
- Extract and reuse the AI models or algorithms
- Remove copyright notices

**See**: `extensions/ai/LICENSE_NOTICE.md` for detailed summary

## 🤝 Why This Licensing Structure?

### Open Core Model

We believe in open source and want to provide a powerful, free framework for the community. The MIT license for Forge core and most extensions ensures:

- Maximum adoption and community growth
- No barriers for startups and small projects
- Transparent, auditable code
- Community contributions and improvements

### Protecting Innovation

The AI Extension represents specialized work that required significant investment in:

- AI model integration research
- Team coordination algorithms
- Training pipeline development
- Production-grade inference systems

The commercial license for the AI Extension allows us to:

- Continue investing in R&D
- Provide enterprise support
- Maintain the extension long-term
- Build a sustainable business model

## 💼 Getting a Commercial License

### Pricing

Contact us for pricing information. We offer:

- **Startup Plans**: Affordable pricing for early-stage companies
- **Enterprise Plans**: Unlimited use with SLA and support
- **Custom Agreements**: Tailored licensing for specific needs

### What's Included

Commercial licenses include:

- ✅ Production deployment rights
- ✅ Commercial use authorization
- ✅ Priority email and chat support
- ✅ Security patch notifications
- ✅ Upgrade assistance
- ✅ Optional: Custom SLAs
- ✅ Optional: Integration consulting

### Contact

- **Email**: licensing@xraph.com
- **Web**: https://github.com/xraph/forge
- **Sales**: Schedule a call via the website

## ❓ FAQ

### Can I use Forge Core with the AI Extension together?

Yes! You can use Forge Core (MIT) in any project. If you want to add the AI Extension:
- Free for personal/evaluation use
- Commercial license required for production use

### What if I only use Forge Core without the AI Extension?

Perfect! The core framework is MIT licensed—use it freely for any purpose, including commercial products.

### Can I contribute to the AI Extension?

Yes! We welcome contributions. By contributing, you grant us a license to use your contribution under any license terms, including the commercial license. Contributors are recognized and appreciated.

### Can I fork and modify Forge Core?

Absolutely! The MIT license allows you to fork, modify, and redistribute the core framework.

### Can I fork and modify the AI Extension?

You can fork and modify for personal use, but you cannot redistribute your fork. See the AI Extension license for details.

### What happens if I violate the AI Extension license?

License termination and potential legal action. We prefer to work with users to ensure compliance—contact us if you have questions.

### Can I get a trial commercial license?

The 90-day evaluation period lets you test the AI Extension internally. Contact us to extend the evaluation or discuss trial licensing.

### Do students/researchers need a commercial license?

No! Academic and research use is explicitly allowed under the free tier.

### What if I'm building an open source project?

For open source projects:
- Forge Core: Use freely (MIT)
- AI Extension: Personal/educational use is free; if the project generates revenue, a commercial license is needed

## 📚 Additional Resources

- **Main License (MIT)**: `/LICENSE`
- **AI Extension License**: `/extensions/ai/LICENSE`
- **AI Extension Summary**: `/extensions/ai/LICENSE_NOTICE.md`
- **Contributing Guide**: `/CONTRIBUTING.md` (if available)
- **Code of Conduct**: `/CODE_OF_CONDUCT.md` (if available)

## 🔄 License Changes

We reserve the right to change licensing terms for future versions. Existing versions remain under their original licenses.

- Current version licensing is locked
- Future versions may have different terms
- You can continue using the version you acquired under its original license

---

**Last Updated**: October 28, 2025

For questions about licensing, contact: licensing@xraph.com

