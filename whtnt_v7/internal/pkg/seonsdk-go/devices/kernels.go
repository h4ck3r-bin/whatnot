package devices

// KernelVersions maps iOS version strings (as returned by UIDevice.systemVersion)
// to all known Darwin kernel version strings for that version.
// Includes release builds, betas, and RCs.
//
// UIDevice.systemVersion returns just the number (e.g. "17.0", "18.1") — it does NOT
// include "beta" or "RC". So all betas/RCs for a given version share the same key.
//
// When a release has no distinct kernel version listed, it shares the kernel from
// its RC or the nearest preceding version.
var KernelVersions = map[string][]string{
	// ===== iOS 17.0 =====
	"17.0": {
		"Darwin Kernel Version 23.0.0: Tue May 23 00:20:52 PDT 2023; root:xnu-10002.0.40.502.3~1/RELEASE_ARM64_T8120",
		"Darwin Kernel Version 23.0.0: Wed Jun 14 21:12:34 PDT 2023; root:xnu-10002.0.116.502.4~2/RELEASE_ARM64_T8120",
		"Darwin Kernel Version 23.0.0: Wed Jun 28 21:02:22 PDT 2023; root:xnu-10002.0.168.502.1~2/RELEASE_ARM64_T8120",
		"Darwin Kernel Version 23.0.0: Fri Jul 14 18:32:55 PDT 2023; root:xnu-10002.0.199.0.3~21/RELEASE_ARM64_T8120",
		"Darwin Kernel Version 23.0.0: Tue Aug 1 08:08:25 PDT 2023; root:xnu-10002.0.242.0.6~33/RELEASE_ARM64_T8120",
		"Darwin Kernel Version 23.0.0: Thu Aug 10 02:08:19 PDT 2023; root:xnu-10002.2.8~2/RELEASE_ARM64_T8120",
		"Darwin Kernel Version 23.0.0: Wed Aug 16 22:20:02 PDT 2023; root:xnu-10002.2.10~3/RELEASE_ARM64_T8120",
		"Darwin Kernel Version 23.0.0: Thu Aug 24 22:03:12 PDT 2023; root:xnu-10002.2.11~3/RELEASE_ARM64_T8120",
		"Darwin Kernel Version 23.0.0: Thu Aug 24 20:19:01 PDT 2023; root:xnu-10002.2.11~1/RELEASE_ARM64_T8120",
		"Darwin Kernel Version 23.0.0: Wed Sep 20 20:15:47 PDT 2023; root:xnu-10002.1.13~1/RELEASE_ARM64_T8120",
	},
	"17.0.1": {
		"Darwin Kernel Version 23.0.0: Fri Sep 15 13:43:33 PDT 2023; root:xnu-10002.2.12~1/RELEASE_ARM64_T8120",
	},
	"17.0.2": {
		"Darwin Kernel Version 23.0.0: Fri Sep 15 13:43:33 PDT 2023; root:xnu-10002.2.12~1/RELEASE_ARM64_T8120",
	},
	"17.0.3": {
		"Darwin Kernel Version 23.0.0: Sat Sep 30 17:17:51 PDT 2023; root:xnu-10002.2.13~1/RELEASE_ARM64_T8122",
	},

	// ===== iOS 17.1 =====
	"17.1": {
		"Darwin Kernel Version 23.1.0: Thu Sep 14 23:31:37 PDT 2023; root:xnu-10002.40.63.502.2~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.1.0: Tue Sep 26 20:15:47 PDT 2023; root:xnu-10002.40.89.502.1~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.1.0: Wed Oct 4 23:01:10 PDT 2023; root:xnu-10002.42.8~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.1.0: Tue Oct 10 02:21:19 PDT 2023; root:xnu-10002.42.9~2/RELEASE_ARM64_T8122",
	},
	"17.1.1": {
		"Darwin Kernel Version 23.1.0: Tue Oct 10 02:21:19 PDT 2023; root:xnu-10002.42.9~2/RELEASE_ARM64_T8122",
	},
	"17.1.2": {
		"Darwin Kernel Version 23.1.0: Tue Oct 10 02:21:19 PDT 2023; root:xnu-10002.42.9~2/RELEASE_ARM64_T8122",
	},

	// ===== iOS 17.2 =====
	"17.2": {
		"Darwin Kernel Version 23.2.0: Mon Oct 16 23:13:23 PDT 2023; root:xnu-10002.60.54.502.1~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.2.0: Fri Nov 3 19:44:04 PDT 2023; root:xnu-10002.60.71.0.3~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.2.0: Tue Nov 14 01:49:16 PST 2023; root:xnu-10002.60.75.0.3~32/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.2.0: Sun Nov 12 06:35:58 PST 2023; root:xnu-10002.60.75.0.3~27/RELEASE_ARM64_T8122",
	},
	"17.2.1": {
		"Darwin Kernel Version 23.2.0: Sun Nov 12 06:35:58 PST 2023; root:xnu-10002.60.75.0.3~27/RELEASE_ARM64_T8122",
	},

	// ===== iOS 17.3 =====
	"17.3": {
		"Darwin Kernel Version 23.3.0: Fri Dec 1 05:58:25 PST 2023; root:xnu-10002.80.11~59/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.3.0: Tue Dec 19 20:53:32 PST 2023; root:xnu-10002.80.20.502.4~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.3.0: Thu Jan 4 12:44:58 PST 2024; root:xnu-10002.82.4~4/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.3.0: Wed Dec 20 17:32:00 PST 2023; root:xnu-10002.82.4~3/RELEASE_ARM64_T8122",
	},
	"17.3.1": {
		"Darwin Kernel Version 23.3.0: Wed Dec 20 17:32:00 PST 2023; root:xnu-10002.82.4~3/RELEASE_ARM64_T8122",
	},

	// ===== iOS 17.4 =====
	"17.4": {
		"Darwin Kernel Version 23.4.0: Wed Jan 17 21:43:43 PST 2024; root:xnu-10063.100.610.0.2~73/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.4.0: Mon Jan 29 06:51:24 PST 2024; root:xnu-10063.100.633~29/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.4.0: Wed Feb 7 21:29:52 PST 2024; root:xnu-10063.100.637.502.3~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.4.0: Thu Feb 15 01:44:29 PST 2024; root:xnu-10063.102.14~2/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.4.0: Fri Feb 16 20:44:36 PST 2024; root:xnu-10063.102.14~10/RELEASE_ARM64_T8122",
	},
	"17.4.1": {
		"Darwin Kernel Version 23.4.0: Fri Mar 8 23:31:42 PST 2024; root:xnu-10063.102.14~67/RELEASE_ARM64_T8122",
	},

	// ===== iOS 17.5 =====
	"17.5": {
		"Darwin Kernel Version 23.5.0: Mon Mar 25 23:03:01 PDT 2024; root:xnu-10063.120.88.502.1~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.5.0: Sun Apr 7 19:26:15 PDT 2024; root:xnu-10063.120.101.0.1~13/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.5.0: Sat Apr 13 16:41:32 PDT 2024; root:xnu-10063.120.104~33/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.5.0: Tue Apr 23 21:28:29 PDT 2024; root:xnu-10063.122.3~2/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.5.0: Wed May 1 20:34:22 PDT 2024; root:xnu-10063.122.3~3/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.5.0: Mon May  6 18:29:24 PDT 2024; root:xnu-10063.121.3~5/RELEASE_ARM64_T8120",
	},
	"17.5.1": {
		"Darwin Kernel Version 23.5.0: Mon May  6 18:29:24 PDT 2024; root:xnu-10063.121.3~5/RELEASE_ARM64_T8120",
	},

	// ===== iOS 17.6 =====
	"17.6": {
		"Darwin Kernel Version 23.6.0: Tue Jun 11 21:25:58 PDT 2024; root:xnu-10063.140.28.502.1~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.6.0: Sun Jun 23 22:43:20 PDT 2024; root:xnu-10063.140.29.0.1~31/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.6.0: Sun Jun 30 20:24:35 PDT 2024; root:xnu-10063.140.33~24/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.6.0: Wed Jul 10 00:32:17 PDT 2024; root:xnu-10063.142.1~2/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 23.6.0: Fri Jul 5 18:04:28 PDT 2024; root:xnu-10063.142.1~1/RELEASE_ARM64_T8122",
	},
	"17.6.1": {
		"Darwin Kernel Version 23.6.0: Fri Jul 5 18:04:28 PDT 2024; root:xnu-10063.142.1~1/RELEASE_ARM64_T8122",
	},

	// ===== iOS 17.7.x =====
	"17.7": {
		"Darwin Kernel Version 23.6.0: Thu Aug 1 00:13:44 PDT 2024; root:xnu-10063.142.1.700.5~1/RELEASE_ARM64_T8122",
	},
	"17.7.1": {
		"Darwin Kernel Version 23.6.0: Thu Sep 12 23:34:20 PDT 2024; root:xnu-10063.142.1.701.1~1/RELEASE_ARM64_T8122",
	},
	"17.7.2": {
		"Darwin Kernel Version 23.6.0: Thu Sep 12 23:34:20 PDT 2024; root:xnu-10063.142.1.701.1~1/RELEASE_ARM64_T8122",
	},
	"17.7.3": {
		"Darwin Kernel Version 23.6.0: Thu Nov 14 21:38:56 PST 2024; root:xnu-10063.142.1.702.7~1/RELEASE_ARM64_T8010",
	},
	"17.7.4": {
		"Darwin Kernel Version 23.6.0: Thu Dec 19 20:34:23 PST 2024; root:xnu-10063.142.1.703.2~1/RELEASE_ARM64_T8010",
	},
	"17.7.5": {
		"Darwin Kernel Version 23.6.0: Thu Dec 19 20:34:23 PST 2024; root:xnu-10063.142.1.703.2~1/RELEASE_ARM64_T8010",
	},
	"17.7.6": {
		"Darwin Kernel Version 23.6.0: Tue Mar 11 21:59:41 PDT 2025; root:xnu-10063.142.1.703.5~1/RELEASE_ARM64_T8010",
	},
	"17.7.7": {
		"Darwin Kernel Version 23.6.0: Thu Apr 24 20:24:42 PDT 2025; root:xnu-10063.142.1.703.7~1/RELEASE_ARM64_T8010",
	},
	"17.7.8": {
		"Darwin Kernel Version 23.6.0: Thu Apr 24 20:24:42 PDT 2025; root:xnu-10063.142.1.703.7~1/RELEASE_ARM64_T8010",
	},
	"17.7.9": {
		"Darwin Kernel Version 23.6.0: Fri Jul 11 17:39:39 PDT 2025; root:xnu-10063.142.1.703.8~1/RELEASE_ARM64_T8010",
	},
	"17.7.10": {
		"Darwin Kernel Version 23.6.0: Fri Jul 11 17:39:39 PDT 2025; root:xnu-10063.142.1.703.8~1/RELEASE_ARM64_T8010",
	},

	// ===== iOS 18.0 =====
	"18.0": {
		"Darwin Kernel Version 24.0.0: Thu May 30 22:34:02 PDT 2024; root:xnu-11215.0.31.522.1~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.0.0: Sun Jun 16 20:49:29 PDT 2024; root:xnu-11215.0.115.502.1~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.0.0: Wed Jul 3 20:40:06 PDT 2024; root:xnu-11215.0.132.502.2~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.0.0: Mon Jul 15 21:22:48 PDT 2024; root:xnu-11215.0.165.502.1~2/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.0.0: Tue Jul 30 20:45:41 PDT 2024; root:xnu-11215.0.199.502.1~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.0.0: Thu Aug 8 01:20:03 PDT 2024; root:xnu-11215.2.5~59/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.0.0: Thu Aug 8 01:17:15 PDT 2024; root:xnu-11215.2.5~62/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.0.0: Mon Aug 12 20:55:14 PDT 2024; root:xnu-11215.1.12~1/RELEASE_ARM64_T8130",
	},
	"18.0.1": {
		"Darwin Kernel Version 24.0.0: Wed Aug 7 23:53:03 PDT 2024; root:xnu-11215.2.5~58/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.0.0: Mon Aug 12 20:55:14 PDT 2024; root:xnu-11215.1.12~1/RELEASE_ARM64_T8130",
	},

	// ===== iOS 18.1 =====
	"18.1": {
		"Darwin Kernel Version 24.1.0: Mon Jul 15 21:19:27 PDT 2024; root:xnu-11215.0.165.502.1~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.1.0: Mon Aug 5 21:29:38 PDT 2024; root:xnu-11215.40.24.502.1~1/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.1.0: Fri Aug 16 19:59:49 PDT 2024; root:xnu-11215.40.42~3/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.1.0: Mon Sep 9 22:31:34 PDT 2024; root:xnu-11215.40.42~93/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.1.0: Mon Sep 2 22:23:47 PDT 2024; root:xnu-11215.40.48~34/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.1.0: Tue Sep 17 10:48:44 PDT 2024; root:xnu-11215.40.59~37/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.1.0: Mon Sep 30 02:02:35 PDT 2024; root:xnu-11215.40.63~38/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.1.0: Mon Oct 7 22:31:15 PDT 2024; root:xnu-11215.42.1~2/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.1.0: Mon Oct 7 21:14:29 PDT 2024; root:xnu-11215.42.1~1/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.1.0: Thu Oct 10 21:05:23 PDT 2024; root:xnu-11215.41.3~2/RELEASE_ARM64_T8130",
	},
	"18.1.1": {
		"Darwin Kernel Version 24.1.0: Mon Oct 7 21:14:29 PDT 2024; root:xnu-11215.42.1~1/RELEASE_ARM64_T8140",
	},

	// ===== iOS 18.2 =====
	"18.2": {
		"Darwin Kernel Version 24.2.0: Wed Oct 16 11:58:49 PDT 2024; root:xnu-11215.60.366.502.3~2/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.2.0: Sat Oct 26 00:59:31 PDT 2024; root:xnu-11215.60.400.0.1~20/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.2.0: Sun Nov 3 22:57:30 PST 2024; root:xnu-11215.60.405~55/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.2.0: Thu Nov 14 22:18:12 PST 2024; root:xnu-11215.62.3~2/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.2.0: Thu Nov 14 22:55:26 PST 2024; root:xnu-11215.62.3~1/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.2.0: Fri Nov 15 18:37:33 PST 2024; root:xnu-11215.61.5~2/RELEASE_ARM64_T8130",
	},
	"18.2.1": {
		"Darwin Kernel Version 24.2.0: Thu Nov 14 22:55:26 PST 2024; root:xnu-11215.62.3~1/RELEASE_ARM64_T8140",
	},

	// ===== iOS 18.3 =====
	"18.3": {
		"Darwin Kernel Version 24.3.0: Mon Dec 9 20:19:15 PST 2024; root:xnu-11215.80.25.502.1~1/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.3.0: Tue Dec 17 22:51:07 PST 2024; root:xnu-11215.80.28.502.2~1/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.3.0: Tue Jan 7 00:51:42 PST 2025; root:xnu-11215.82.4~9/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.3.0: Thu Jan 16 03:00:11 PST 2025; root:xnu-11215.82.4~20/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.3.0: Thu Jan  2 20:24:41 PST 2025; root:xnu-11215.81.4~3/RELEASE_ARM64_T8130",
	},
	"18.3.1": {
		"Darwin Kernel Version 24.3.0: Thu Jan 16 03:00:11 PST 2025; root:xnu-11215.82.4~20/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.3.0: Sun Dec 22 20:42:22 PST 2024; root:xnu-11215.82.4~2/RELEASE_ARM64_T8140",
	},
	"18.3.2": {
		"Darwin Kernel Version 24.3.0: Thu Jan 16 03:00:11 PST 2025; root:xnu-11215.82.4~20/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.3.0: Sun Dec 22 20:40:10 PST 2024; root:xnu-11215.82.4~3/RELEASE_ARM64_T8122",
		"Darwin Kernel Version 24.3.0: Sun Dec 22 20:42:22 PST 2024; root:xnu-11215.82.4~2/RELEASE_ARM64_T8140",
	},

	// ===== iOS 18.4 =====
	"18.4": {
		"Darwin Kernel Version 24.4.0: Sun Feb 16 02:28:25 PST 2025; root:xnu-11417.100.533.502.4~3/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.4.0: Tue Feb 25 21:30:30 PST 2025; root:xnu-11417.100.564.502.1~1/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.4.0: Tue Mar 4 21:48:46 PST 2025; root:xnu-11417.100.576.502.2~1/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.4.0: Tue Mar 11 22:26:05 PDT 2025; root:xnu-11417.102.9~2/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.4.0: Sat Mar 15 18:26:55 PDT 2025; root:xnu-11417.102.9~20/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.4.0: Wed Feb 19 20:26:08 PST 2025; root:xnu-11215.101.2~1/RELEASE_ARM64_T8130",
	},
	"18.4.1": {
		"Darwin Kernel Version 24.4.0: Sat Mar 15 18:26:55 PDT 2025; root:xnu-11417.102.9~20/RELEASE_ARM64_T8140",
	},

	// ===== iOS 18.5 =====
	"18.5": {
		"Darwin Kernel Version 24.5.0: Mon Mar 24 20:10:57 PDT 2025; root:xnu-11417.120.72.0.4~14/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.5.0: Thu Apr 10 21:44:12 PDT 2025; root:xnu-11417.120.96.502.1~1/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.5.0: Tue Apr 15 21:27:46 PDT 2025; root:xnu-11417.120.105.502.1~1/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.5.0: Tue Apr 22 21:26:13 PDT 2025; root:xnu-11417.122.4~2/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.5.0: Tue Apr 22 20:38:09 PDT 2025; root:xnu-11417.122.4~1/RELEASE_ARM64_T8140",
	},

	// ===== iOS 18.6 =====
	"18.6": {
		"Darwin Kernel Version 24.6.0: Wed Jun 11 21:19:19 PDT 2025; root:xnu-11417.140.62.502.1~1/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.6.0: Tue Jun 17 20:58:13 PDT 2025; root:xnu-11417.140.64.0.1~33/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.6.0: Thu Jul 3 02:18:09 PDT 2025; root:xnu-11417.140.66~43/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.6.0: Tue Jul 15 21:54:57 PDT 2025; root:xnu-11417.140.69~16/RELEASE_ARM64_T8140",
	},
	"18.6.1": {
		"Darwin Kernel Version 24.6.0: Tue Jul 15 21:54:57 PDT 2025; root:xnu-11417.140.69~16/RELEASE_ARM64_T8140",
	},
	"18.6.2": {
		"Darwin Kernel Version 24.6.0: Tue Jul 15 21:54:57 PDT 2025; root:xnu-11417.140.69~16/RELEASE_ARM64_T8140",
	},

	// ===== iOS 18.7.x =====
	"18.7": {
		"Darwin Kernel Version 24.6.0: Tue Aug 12 00:02:51 PDT 2025; root:xnu-11417.140.69.700.6~5/RELEASE_ARM64_T8140",
	},
	"18.7.1": {
		"Darwin Kernel Version 24.6.0: Tue Aug 12 00:02:51 PDT 2025; root:xnu-11417.140.69.700.6~5/RELEASE_ARM64_T8140",
	},
	"18.7.2": {
		"Darwin Kernel Version 24.6.0: Wed Oct 15 21:15:52 PDT 2025; root:xnu-11417.140.69.702.20~1/RELEASE_ARM64_T8140",
	},
	"18.7.3": {
		"Darwin Kernel Version 24.6.0: Wed Nov 5 21:32:36 PST 2025; root:xnu-11417.140.69.704.2~1/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 24.6.0: Wed Nov 5 21:28:58 PST 2025; root:xnu-11417.140.69.704.2~1/RELEASE_ARM64_T8020",
	},
	"18.7.4": {
		"Darwin Kernel Version 24.6.0: Wed Nov 5 21:28:58 PST 2025; root:xnu-11417.140.69.704.2~1/RELEASE_ARM64_T8020",
	},
	"18.7.5": {
		"Darwin Kernel Version 24.6.0: Mon Jan 19 22:05:19 PST 2026; root:xnu-11417.140.69.706.3~1/RELEASE_ARM64_T8020",
	},
	"18.7.6": {
		"Darwin Kernel Version 24.6.0: Mon Jan 19 22:05:19 PST 2026; root:xnu-11417.140.69.706.3~1/RELEASE_ARM64_T8020",
	},
	"18.7.7": {
		"Darwin Kernel Version 24.6.0: Wed Mar 4 22:11:45 PST 2026; root:xnu-11417.140.69.706.9~1/RELEASE_ARM64_T8020",
	},

	// ===== iOS 26.0 =====
	"26.0": {
		"Darwin Kernel Version 25.0.0: Fri May 30 19:47:39 PDT 2025; root:xnu-12377.0.81.0.3~311/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 25.0.0: Tue Jun 17 00:11:51 PDT 2025; root:xnu-12377.0.122.0.1~121/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 25.0.0: Mon Jun 30 21:18:14 PDT 2025; root:xnu-12377.0.132.504.1~1/RELEASE_ARM64_T8310",
		"Darwin Kernel Version 25.0.0: Tue Jul 15 00:15:15 PDT 2025; root:xnu-12377.0.154.0.2~118/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 25.0.0: Fri Jul 25 21:57:07 PDT 2025; root:xnu-12377.0.187.0.2~18/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 25.0.0: Tue Aug 5 22:37:43 PDT 2025; root:xnu-12377.2.6~4/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 25.0.0: Fri Aug 15 00:02:28 PDT 2025; root:xnu-12377.2.7~9/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 25.0.0: Wed Aug 27 20:44:56 PDT 2025; root:xnu-12377.2.8~3/RELEASE_ARM64_T8140",
		"Darwin Kernel Version 25.0.0: Tue Aug 26 20:30:19 PDT 2025; root:xnu-12377.2.8~1/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.0.0: Thu Aug 14 23:14:12 PDT 2025; root:xnu-12377.2.7~7/RELEASE_ARM64_T8150",
	},
	"26.0.1": {
		"Darwin Kernel Version 25.0.0: Thu Sep 25 15:05:47 PDT 2025; root:xnu-12377.2.9~1/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.0.0: Sun Oct 12 12:47:36 PDT 2025; root:xnu-12377.2.9~32/RELEASE_ARM64_T8142",
	},

	// ===== iOS 26.1 =====
	"26.1": {
		"Darwin Kernel Version 25.1.0: Tue Sep 16 20:39:23 PDT 2025; root:xnu-12377.40.77.502.3~1/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.1.0: Wed Oct 1 21:35:00 PDT 2025; root:xnu-12377.40.103.502.2~2/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.1.0: Wed Oct 8 22:49:46 PDT 2025; root:xnu-12377.40.120.502.1~2/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.1.0: Thu Oct 16 23:19:04 PDT 2025; root:xnu-12377.42.6~13/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.1.0: Thu Oct 23 11:12:58 PDT 2025; root:xnu-12377.42.6~55/RELEASE_ARM64_T8150",
	},

	// ===== iOS 26.2 =====
	"26.2": {
		"Darwin Kernel Version 25.2.0: Tue Oct 28 20:54:34 PDT 2025; root:xnu-12377.60.50.502.2~1/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.2.0: Mon Nov 3 21:14:41 PST 2025; root:xnu-12377.60.50.0.6~45/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.2.0: Wed Nov 12 22:20:37 PST 2025; root:xnu-12377.62.9~13/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.2.0: Tue Nov 18 21:11:04 PST 2025; root:xnu-12377.62.10~1/RELEASE_ARM64_T8150",
	},
	"26.2.1": {
		"Darwin Kernel Version 25.2.0: Fri Jan 9 18:29:04 PST 2026; root:xnu-12377.62.10~267/RELEASE_ARM64_T8150",
	},

	// ===== iOS 26.3 =====
	"26.3": {
		"Darwin Kernel Version 25.3.0: Thu Dec 4 23:52:39 PST 2025; root:xnu-12377.80.260.0.1~30/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.3.0: Mon Jan 5 20:28:34 PST 2026; root:xnu-12377.80.267~105/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.3.0: Thu Jan 22 21:38:36 PST 2026; root:xnu-12377.80.273.502.1~3/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.3.0: Mon Jan 26 20:59:54 PST 2026; root:xnu-12377.82.2~2/RELEASE_ARM64_T8150",
	},
	"26.3.1": {
		"Darwin Kernel Version 25.3.0: Wed Feb 4 23:04:24 PST 2026; root:xnu-12377.82.2~13/RELEASE_ARM64_T8150",
	},

	// ===== iOS 26.4 =====
	"26.4": {
		"Darwin Kernel Version 25.4.0: Wed Feb 4 23:07:23 PST 2026; root:xnu-12377.100.591.502.1~2/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.4.0: Sun Feb 15 23:07:24 PST 2026; root:xnu-12377.100.625.0.3~14/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.4.0: Thu Feb 26 23:19:49 PST 2026; root:xnu-12377.100.630.502.1~1/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.4.0: Fri Mar 6 00:08:59 PST 2026; root:xnu-12377.102.10~4/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.4.0: Thu Mar 5 23:59:10 PST 2026; root:xnu-12377.102.10~3/RELEASE_ARM64_T8150",
	},
	"26.4.1": {
		"Darwin Kernel Version 25.4.0: Thu Mar 5 23:59:10 PST 2026; root:xnu-12377.102.10~3/RELEASE_ARM64_T8150",
	},

	// ===== iOS 26.5 =====
	"26.5": {
		"Darwin Kernel Version 25.5.0: Fri Mar 20 18:55:25 PDT 2026; root:xnu-12377.120.72.0.4~14/RELEASE_ARM64_T8150",
		"Darwin Kernel Version 25.5.0: Sun Apr 5 22:14:26 PDT 2026; root:xnu-12377.120.91.0.1~12/RELEASE_ARM64_T8150",
	},
}
